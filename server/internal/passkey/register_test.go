package passkey_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

const (
	testOrigin = "http://localhost:5173"
	testRPID   = "localhost"
	base       = "/api/v1/auth/passkeys"
)

var b64 = base64.RawURLEncoding

// ---- メモリ上の依存 ----

type memPasskeys struct {
	mu      sync.Mutex
	nextID  int64
	handles map[int64][]byte
	creds   []passkey.Credential
	// 障害の注入。非nilなら、その操作がDB障害として失敗する
	findErr, handleErr, recordErr error
}

func newMemPasskeys() *memPasskeys { return &memPasskeys{handles: map[int64][]byte{}} }

func (m *memPasskeys) UserHandle(_ context.Context, userID int64, fresh []byte) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.handles[userID]; ok {
		return h, nil
	}
	m.handles[userID] = fresh
	return fresh, nil
}

func (m *memPasskeys) UserIDByHandle(_ context.Context, handle []byte) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.handleErr != nil {
		return 0, m.handleErr
	}
	for id, h := range m.handles {
		if len(handle) > 0 && bytes.Equal(h, handle) {
			return id, nil
		}
	}
	return 0, passkey.ErrNotFound
}

func (m *memPasskeys) Create(_ context.Context, c passkey.Credential) (passkey.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.creds {
		if bytes.Equal(e.CredentialID, c.CredentialID) {
			return passkey.Credential{}, passkey.ErrCredentialExists
		}
		if e.UserID == c.UserID {
			n++
		}
	}
	if n >= passkey.MaxPasskeysPerUser {
		return passkey.Credential{}, passkey.ErrLimitReached
	}
	m.nextID++
	c.ID = m.nextID
	c.CreatedAt = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	m.creds = append(m.creds, c)
	return c, nil
}

func (m *memPasskeys) ListByUser(_ context.Context, userID int64) ([]passkey.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []passkey.Credential
	for _, c := range m.creds {
		if c.UserID == userID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (m *memPasskeys) FindByCredentialID(_ context.Context, credentialID []byte) (passkey.Credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.findErr != nil {
		return passkey.Credential{}, m.findErr
	}
	for _, c := range m.creds {
		if bytes.Equal(c.CredentialID, credentialID) {
			return c, nil
		}
	}
	return passkey.Credential{}, passkey.ErrNotFound
}

// RecordUse はDBの UpdatePasskeyUsage と同じ条件で更新する: 認証回数が増えたとき、または0のまま変わらないときだけ。
func (m *memPasskeys) RecordUse(_ context.Context, credentialID []byte, signCount uint32, backupState bool, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.recordErr != nil {
		return m.recordErr
	}
	for i := range m.creds {
		c := &m.creds[i]
		if bytes.Equal(c.CredentialID, credentialID) && (c.SignCount < signCount || (c.SignCount == 0 && signCount == 0)) {
			c.SignCount, c.BackupState = signCount, backupState
			used := now
			c.LastUsedAt = &used
			return nil
		}
	}
	return passkey.ErrNotFound
}

// byCredentialID は保存されている状態を返す(試験の確認用)。
func (m *memPasskeys) byCredentialID(t *testing.T, credentialID []byte) passkey.Credential {
	t.Helper()
	c, err := m.FindByCredentialID(context.Background(), credentialID)
	if err != nil {
		t.Fatalf("保存されたパスキーが無い: %v", err)
	}
	return c
}

func (m *memPasskeys) Rename(_ context.Context, userID, id int64, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.creds {
		if m.creds[i].ID == id && m.creds[i].UserID == userID {
			m.creds[i].Name = name
			return nil
		}
	}
	return passkey.ErrNotFound
}

func (m *memPasskeys) Delete(_ context.Context, userID, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.creds {
		if m.creds[i].ID == id && m.creds[i].UserID == userID {
			m.creds = append(m.creds[:i], m.creds[i+1:]...)
			return nil
		}
	}
	return passkey.ErrNotFound
}

func (m *memPasskeys) count(userID int64) int {
	l, _ := m.ListByUser(context.Background(), userID)
	return len(l)
}

type memChallenge struct {
	purpose passkey.Purpose
	userID  *int64
	data    []byte
	expires time.Time
	used    bool
}

type memChallenges struct {
	mu   sync.Mutex
	rows map[[32]byte]*memChallenge
	// 非nilなら Consume がDB障害として失敗する
	consumeErr error
}

func (m *memChallenges) Create(_ context.Context, id [32]byte, p passkey.Purpose, userID *int64, data []byte, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[id] = &memChallenge{purpose: p, userID: userID, data: data, expires: exp}
	return nil
}

func (m *memChallenges) Consume(_ context.Context, id [32]byte, p passkey.Purpose, now time.Time) (passkey.Challenge, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.consumeErr != nil {
		return passkey.Challenge{}, m.consumeErr
	}
	r, ok := m.rows[id]
	if !ok || r.used || r.purpose != p || !r.expires.After(now) {
		return passkey.Challenge{}, passkey.ErrChallengeInvalid
	}
	r.used = true
	return passkey.Challenge{UserID: r.userID, SessionData: r.data}, nil
}

func (m *memChallenges) DeleteExpired(_ context.Context, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.rows {
		if !r.expires.After(now) || r.used {
			delete(m.rows, k)
		}
	}
	return nil
}

type memSessions struct{ rows map[[32]byte]auth.Session }

func (m memSessions) Create(context.Context, [32]byte, int64, time.Time) error { return nil }
func (m memSessions) FindUser(_ context.Context, h [32]byte, now time.Time) (auth.Session, error) {
	s, ok := m.rows[h]
	if !ok || !s.ExpiresAt.After(now) {
		return auth.Session{}, auth.ErrNotFound
	}
	return s, nil
}
func (m memSessions) Delete(context.Context, [32]byte) error         { return nil }
func (m memSessions) DeleteExpired(context.Context, time.Time) error { return nil }

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

// ---- テスト環境 ----

type env struct {
	t        *testing.T
	h        http.Handler
	clock    *clock
	passkeys *memPasskeys
	ch       *memChallenges
	logs     *bytes.Buffer
}

func newEnv(t *testing.T) *env {
	t.Helper()
	clk := &clock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	w, err := passkey.NewWebAuthn(passkey.Config{RPID: testRPID, RPDisplayName: "MyTechPulse", Origins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	pk := newMemPasskeys()
	ch := &memChallenges{rows: map[[32]byte]*memChallenge{}}
	svc := &passkey.Service{WebAuthn: w, Passkeys: pk, Challenges: ch, Clock: clk, Logger: logger}
	sessions := memSessions{rows: map[[32]byte]auth.Session{}}
	for name, id := range map[string]int64{"tok-alice": 1, "tok-bob": 2} {
		sessions.rows[auth.HashToken(name)] = auth.Session{
			User:      auth.User{ID: id, Username: map[int64]string{1: "alice", 2: "bob"}[id], Role: "member"},
			ExpiresAt: clk.now.Add(time.Hour),
		}
	}
	authSvc := &auth.Service{Sessions: sessions, Clock: clk}
	h, _ := app.New(config.Config{Environment: "local", CookieName: "mtp_session", CORSOrigins: []string{testOrigin}},
		app.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: authSvc, Passkey: svc})
	return &env{t: t, h: h, clock: clk, passkeys: pk, ch: ch, logs: logs}
}

type resp struct {
	*httptest.ResponseRecorder
}

// do は要求を1つ送る。challenge が空でなければ、本文(map)の challengeToken に入れる。Cookieは送らない。
func (e *env) do(method, path, session, challenge string, body any, csrf bool) resp {
	e.t.Helper()
	if m, ok := body.(map[string]any); ok && challenge != "" {
		withToken := map[string]any{"challengeToken": challenge}
		for k, v := range m {
			withToken[k] = v
		}
		body = withToken
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", testOrigin)
		if csrf {
			req.Header.Set("X-MTP-CSRF", "1")
		}
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: session})
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

func (r resp) json() map[string]any {
	var m map[string]any
	_ = json.Unmarshal(r.Body.Bytes(), &m)
	return m
}

func (r resp) code() string {
	c, _ := r.json()["code"].(string)
	return c
}

// noCookies は、応答がCookieを一切設定していないことを確かめる(チャレンジはCookieで運ばない)。
func (r resp) noCookies(t *testing.T, what string) {
	t.Helper()
	if got := r.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("%s: Set-Cookie を付けてはいけない: %v", what, got)
	}
}

// begin は登録を開始し、本文のチャレンジトークンと端末へ渡す設定を返す。
func (e *env) begin(session string) (string, map[string]any, resp) {
	e.t.Helper()
	r := e.do(http.MethodPost, base+"/register/begin", session, "", nil, true)
	if r.Code != http.StatusOK {
		return "", nil, r
	}
	token, _ := r.json()["challengeToken"].(string)
	if token == "" {
		e.t.Fatal("本文にチャレンジトークンが無い")
	}
	pub, _ := r.json()["publicKey"].(map[string]any)
	return token, pub, r
}

// ---- ソフトウェア認証器 ----

func cosePublicKey(pub *ecdsa.PublicKey) []byte {
	x, y := pub.X.FillBytes(make([]byte, 32)), pub.Y.FillBytes(make([]byte, 32))
	out := []byte{0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20}
	out = append(out, x...)
	out = append(out, 0x22, 0x58, 0x20)
	return append(out, y...)
}

// attest は「none」形式の登録応答を、ブラウザが返すJSON表現で作る。
func attest(t *testing.T, pub map[string]any, credID []byte, origin string) map[string]any {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rpHash := sha256.Sum256([]byte(testRPID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, 0x45) // UP | UV | AT
	authData = append(authData, 0, 0, 0, 0)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(credID)))
	authData = append(authData, credID...)
	authData = append(authData, cosePublicKey(&key.PublicKey)...)

	att := []byte{0xa3, 0x63}
	att = append(att, "fmt"...)
	att = append(att, 0x64)
	att = append(att, "none"...)
	att = append(att, 0x67)
	att = append(att, "attStmt"...)
	att = append(att, 0xa0, 0x68)
	att = append(att, "authData"...)
	att = append(att, 0x59)
	att = binary.BigEndian.AppendUint16(att, uint16(len(authData)))
	att = append(att, authData...)

	clientData, _ := json.Marshal(map[string]any{
		"type": "webauthn.create", "challenge": pub["challenge"], "origin": origin, "crossOrigin": false,
	})
	return map[string]any{
		"id": b64.EncodeToString(credID), "rawId": b64.EncodeToString(credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"attestationObject": b64.EncodeToString(att),
			"transports":        []string{"internal"},
		},
		"clientExtensionResults": map[string]any{},
	}
}

func newCredID() []byte {
	id := make([]byte, 32)
	_, _ = rand.Read(id)
	return id
}

func (e *env) finish(session, challenge, name string, cred map[string]any) resp {
	e.t.Helper()
	body := map[string]any{"credential": cred}
	if name != "" {
		body["name"] = name
	}
	return e.do(http.MethodPost, base+"/register/finish", session, challenge, body, true)
}

func (e *env) register(session, name string) (int64, []byte) {
	e.t.Helper()
	token, pub, r := e.begin(session)
	if r.Code != http.StatusOK {
		e.t.Fatalf("begin = %d %s", r.Code, r.Body)
	}
	id := newCredID()
	fr := e.finish(session, token, name, attest(e.t, pub, id, testOrigin))
	if fr.Code != http.StatusCreated {
		e.t.Fatalf("finish = %d %s", fr.Code, fr.Body)
	}
	return int64(fr.json()["id"].(float64)), id
}

// ---- テスト ----

func TestRegisterFlow(t *testing.T) {
	e := newEnv(t)
	token, pub, r := e.begin("tok-alice")
	if r.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", r.Code, r.Body)
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Error("beginの応答はキャッシュさせない")
	}
	r.noCookies(t, "begin")
	if pub["rp"].(map[string]any)["id"] != testRPID {
		t.Errorf("rp = %v", pub["rp"])
	}
	sel := pub["authenticatorSelection"].(map[string]any)
	if sel["userVerification"] != "required" || sel["residentKey"] != "required" {
		t.Errorf("authenticatorSelection = %v", sel)
	}
	if ex, _ := pub["excludeCredentials"].([]any); len(ex) != 0 {
		t.Errorf("最初は除外なし: %v", ex)
	}
	// ユーザーハンドルは利用者IDから作らない32バイトの乱数
	rawID, err := b64.DecodeString(pub["user"].(map[string]any)["id"].(string))
	if err != nil || len(rawID) != 32 || bytes.Equal(rawID, []byte("1")) {
		t.Errorf("user.id = %x err=%v", rawID, err)
	}
	if string(token) == pub["challenge"] {
		t.Error("トークンとWebAuthnのチャレンジは別物")
	}

	credID := newCredID()
	fr := e.finish("tok-alice", token, "iPhone", attest(t, pub, credID, testOrigin))
	if fr.Code != http.StatusCreated {
		t.Fatalf("finish = %d %s", fr.Code, fr.Body)
	}
	fr.noCookies(t, "finish")
	body := fr.json()
	if body["name"] != "iPhone" {
		t.Errorf("body = %v", body)
	}
	for _, secret := range []string{"public_key", "credential_id", "publicKey"} {
		if _, ok := body[secret]; ok {
			t.Errorf("応答に %s を含めない", secret)
		}
	}

	list := e.do(http.MethodGet, base, "tok-alice", "", nil, false)
	if list.Code != http.StatusOK || list.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("list = %d", list.Code)
	}
	items := list.json()["passkeys"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "iPhone" {
		t.Fatalf("items = %v", items)
	}
	if _, ok := items[0].(map[string]any)["last_used_at"]; ok {
		t.Error("未使用なら last_used_at を省略する")
	}

	// 2回目のbeginでは、登録済みの認証情報が excludeCredentials に入る
	_, pub2, _ := e.begin("tok-alice")
	ex := pub2["excludeCredentials"].([]any)
	if len(ex) != 1 || ex[0].(map[string]any)["id"] != b64.EncodeToString(credID) {
		t.Errorf("excludeCredentials = %v", ex)
	}
	// ハンドルは2回目も同じ
	if pub2["user"].(map[string]any)["id"] != pub["user"].(map[string]any)["id"] {
		t.Error("ユーザーハンドルは変わらない")
	}
}

func TestRegisterDefaultName(t *testing.T) {
	e := newEnv(t)
	e.register("tok-alice", "")
	l, _ := e.passkeys.ListByUser(context.Background(), 1)
	if len(l) != 1 || l[0].Name != passkey.DefaultName {
		t.Fatalf("name = %+v", l)
	}
}

func TestRequiresLoginAndCSRF(t *testing.T) {
	e := newEnv(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, base + "/register/begin"},
		{http.MethodPost, base + "/register/finish"},
		{http.MethodGet, base},
		{http.MethodPatch, base + "/1"},
		{http.MethodDelete, base + "/1"},
	} {
		var body any
		if c.method == http.MethodPatch {
			body = map[string]any{"name": "x"}
		}
		if c.path == base+"/register/finish" {
			body = map[string]any{"challengeToken": "t", "credential": map[string]any{}}
		}
		if r := e.do(c.method, c.path, "", "", body, true); r.Code != http.StatusUnauthorized || r.code() != "unauthenticated" {
			t.Errorf("%s %s 未ログイン = %d %s", c.method, c.path, r.Code, r.Body)
		}
		if r := e.do(c.method, c.path, "tok-nope", "", body, true); r.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 不正なセッション = %d", c.method, c.path, r.Code)
		}
		if c.method != http.MethodGet {
			if r := e.do(c.method, c.path, "tok-alice", "", body, false); r.Code != http.StatusForbidden || r.code() != "csrf_header_required" {
				t.Errorf("%s %s CSRFヘッダー無し = %d %s", c.method, c.path, r.Code, r.Body)
			}
		}
	}
}

func TestChallengeIsSingleUse(t *testing.T) {
	e := newEnv(t)
	token, pub, _ := e.begin("tok-alice")
	cred := attest(t, pub, newCredID(), testOrigin)
	if r := e.finish("tok-alice", token, "", cred); r.Code != http.StatusCreated {
		t.Fatalf("1回目 = %d %s", r.Code, r.Body)
	}
	// 同じチャレンジでの再送は、別の認証情報でも拒否
	r := e.finish("tok-alice", token, "", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("再利用 = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != 1 {
		t.Error("再利用で増えてはいけない")
	}
}

func TestChallengeExpires(t *testing.T) {
	e := newEnv(t)
	token, pub, _ := e.begin("tok-alice")
	e.clock.now = e.clock.now.Add(passkey.ChallengeTTL + time.Second)
	r := e.finish("tok-alice", token, "", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("期限切れ = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != 0 {
		t.Error("期限切れで登録されてはいけない")
	}
}

func TestChallengeMissingOrUnknown(t *testing.T) {
	e := newEnv(t)
	_, pub, _ := e.begin("tok-alice")
	cred := attest(t, pub, newCredID(), testOrigin)
	// 形は正しいが、保存されていないトークンは、チャレンジの不正として400
	for name, token := range map[string]string{
		"未知のトークン":   "unknown",
		"形式の違うトークン": "!!not base64url!!",
		"空白だけ":      "   ",
	} {
		r := e.finish("tok-alice", token, "", cred)
		if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
			t.Errorf("%s = %d %s", name, r.Code, r.Body)
		}
	}
	// 欠落・空・長すぎるトークンは、入力の形の不正として、手続きに入る前に422で拒否する
	finishPath := base + "/register/finish"
	cb, _ := json.Marshal(cred)
	for name, body := range map[string]string{
		"欠落":     `{"credential":` + string(cb) + `}`,
		"空":      `{"challengeToken":"","credential":` + string(cb) + `}`,
		"長すぎる":   `{"challengeToken":"` + strings.Repeat("a", 129) + `","credential":` + string(cb) + `}`,
		"文字列でない": `{"challengeToken":5,"credential":` + string(cb) + `}`,
	} {
		r := e.rawPost(finishPath, body, "tok-alice")
		if r.Code != http.StatusUnprocessableEntity || r.code() != "validation_failed" {
			t.Errorf("トークン%s = %d %s", name, r.Code, r.Body)
		}
	}
}

// rawPost は本文をそのまま送る(Cookieはセッションだけ)。
func (e *env) rawPost(path, body, session string) resp {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-MTP-CSRF", "1")
	req.AddCookie(&http.Cookie{Name: "mtp_session", Value: session})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

// 同じ利用者が2つのタブで同時に登録を始めても、互いのトークンは影響しない。
func TestTwoRegisterCeremoniesAreIndependent(t *testing.T) {
	e := newEnv(t)
	tokenA, pubA, _ := e.begin("tok-alice")
	tokenB, pubB, _ := e.begin("tok-alice")
	if tokenA == tokenB {
		t.Fatal("ceremonyごとに別のトークンを返すこと")
	}
	// B → A の順に完了する。先に開いたAも成功する
	if r := e.finish("tok-alice", tokenB, "b", attest(t, pubB, newCredID(), testOrigin)); r.Code != http.StatusCreated {
		t.Fatalf("B = %d %s", r.Code, r.Body)
	}
	if r := e.finish("tok-alice", tokenA, "a", attest(t, pubA, newCredID(), testOrigin)); r.Code != http.StatusCreated {
		t.Fatalf("A = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != 2 {
		t.Errorf("2件登録されるはず: %d", e.passkeys.count(1))
	}
}

// 別のceremonyのトークンを使うと、そのトークンだけが消費され、他方は残る。
func TestRegisterTokenOfOneCeremonyDoesNotConsumeTheOther(t *testing.T) {
	e := newEnv(t)
	tokenA, pubA, _ := e.begin("tok-alice")
	tokenB, pubB, _ := e.begin("tok-alice")
	// Aのトークンを使うが、Bの設定に対する応答を送る(チャレンジ不一致で失敗し、Aは消費される)
	if r := e.finish("tok-alice", tokenA, "", attest(t, pubB, newCredID(), testOrigin)); r.Code != http.StatusBadRequest || r.code() != "passkey_verification_failed" {
		t.Fatalf("取り違え = %d %s", r.Code, r.Body)
	}
	if r := e.finish("tok-alice", tokenA, "", attest(t, pubA, newCredID(), testOrigin)); r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("Aは使い切り = %d %s", r.Code, r.Body)
	}
	if r := e.finish("tok-alice", tokenB, "", attest(t, pubB, newCredID(), testOrigin)); r.Code != http.StatusCreated {
		t.Fatalf("Bは残っている = %d %s", r.Code, r.Body)
	}
}

// ログイン用のトークンでは登録できない。
func TestLoginTokenCannotFinishRegistration(t *testing.T) {
	e := newEnv(t)
	tokenLogin, id, err := passkey.NewChallengeToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ch.Create(context.Background(), id, passkey.PurposeLogin, nil, []byte("{}"), e.clock.now.Add(passkey.ChallengeTTL)); err != nil {
		t.Fatal(err)
	}
	_, pub, _ := e.begin("tok-alice")
	r := e.finish("tok-alice", tokenLogin, "", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("login用トークンでregister = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != 0 {
		t.Error("登録されてはいけない")
	}
}

func TestChallengeBelongsToTheUserWhoBegan(t *testing.T) {
	e := newEnv(t)
	token, pub, _ := e.begin("tok-alice")
	r := e.finish("tok-bob", token, "", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("他人のチャレンジ = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1)+e.passkeys.count(2) != 0 {
		t.Error("誰にも登録されてはいけない")
	}
}

func TestWrongOriginOrChallengeIsRejectedAndBurnsChallenge(t *testing.T) {
	e := newEnv(t)
	token, pub, _ := e.begin("tok-alice")
	r := e.finish("tok-alice", token, "", attest(t, pub, newCredID(), "https://evil.example"))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_verification_failed" {
		t.Fatalf("オリジン不一致 = %d %s", r.Code, r.Body)
	}
	// 失敗してもチャレンジは使い切る
	r = e.finish("tok-alice", token, "", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("失敗後の再利用 = %d %s", r.Code, r.Body)
	}

	token, pub, _ = e.begin("tok-alice")
	other := map[string]any{"challenge": b64.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))}
	r = e.finish("tok-alice", token, "", attest(t, other, newCredID(), testOrigin))
	if r.Code != http.StatusBadRequest || r.code() != "passkey_verification_failed" {
		t.Fatalf("チャレンジ不一致 = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != 0 {
		t.Error("検証に失敗したら登録されない")
	}
	_ = pub
}

func TestMalformedCredentialIsRejected(t *testing.T) {
	e := newEnv(t)
	token, _, _ := e.begin("tok-alice")
	r := e.finish("tok-alice", token, "", map[string]any{"id": "x", "type": "public-key"})
	if r.Code != http.StatusBadRequest || r.code() != "passkey_verification_failed" {
		t.Fatalf("壊れた応答 = %d %s", r.Code, r.Body)
	}
}

func TestDuplicateCredentialIsConflict(t *testing.T) {
	e := newEnv(t)
	_, id := e.register("tok-alice", "a")
	token, pub, _ := e.begin("tok-bob")
	r := e.finish("tok-bob", token, "", attest(t, pub, id, testOrigin))
	if r.Code != http.StatusConflict || r.code() != "passkey_already_registered" {
		t.Fatalf("重複 = %d %s", r.Code, r.Body)
	}
}

func TestLimitIsTen(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < passkey.MaxPasskeysPerUser; i++ {
		e.register("tok-alice", "p"+strconv.Itoa(i))
	}
	_, _, r := e.begin("tok-alice")
	if r.Code != http.StatusConflict || r.code() != "passkey_limit_reached" {
		t.Fatalf("11件目のbegin = %d %s", r.Code, r.Body)
	}
	// 他の利用者には影響しない
	if _, _, r := e.begin("tok-bob"); r.Code != http.StatusOK {
		t.Fatalf("別の利用者 = %d", r.Code)
	}
}

func TestLimitIsCheckedAtFinishToo(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < passkey.MaxPasskeysPerUser-1; i++ {
		e.register("tok-alice", "p")
	}
	token, pub, _ := e.begin("tok-alice")
	e.register("tok-alice", "last") // beginの後に別の端末で10件目を登録
	r := e.finish("tok-alice", token, "", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusConflict || r.code() != "passkey_limit_reached" {
		t.Fatalf("競合 = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != passkey.MaxPasskeysPerUser {
		t.Errorf("件数 = %d", e.passkeys.count(1))
	}
}

func TestRenameAndDelete(t *testing.T) {
	e := newEnv(t)
	id, _ := e.register("tok-alice", "old")
	path := base + "/" + strconv.FormatInt(id, 10)

	if r := e.do(http.MethodPatch, path, "tok-alice", "", map[string]any{"name": "  new name  "}, true); r.Code != http.StatusNoContent {
		t.Fatalf("rename = %d %s", r.Code, r.Body)
	}
	l, _ := e.passkeys.ListByUser(context.Background(), 1)
	if l[0].Name != "new name" {
		t.Errorf("name = %q", l[0].Name)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("あ", 65)} {
		r := e.do(http.MethodPatch, path, "tok-alice", "", map[string]any{"name": bad}, true)
		if r.Code != http.StatusUnprocessableEntity {
			t.Errorf("名前 %q = %d", bad, r.Code)
		}
	}
	if r := e.do(http.MethodPatch, path, "tok-alice", "", map[string]any{"name": strings.Repeat("あ", 64)}, true); r.Code != http.StatusNoContent {
		t.Errorf("64文字 = %d", r.Code)
	}
	if r := e.do(http.MethodDelete, path, "tok-alice", "", nil, true); r.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", r.Code, r.Body)
	}
	if e.passkeys.count(1) != 0 {
		t.Error("物理削除される")
	}
	if r := e.do(http.MethodDelete, path, "tok-alice", "", nil, true); r.Code != http.StatusNotFound {
		t.Errorf("削除済み = %d", r.Code)
	}
}

func TestOthersPasskeysAreNotFound(t *testing.T) {
	e := newEnv(t)
	id, _ := e.register("tok-alice", "alice's")
	path := base + "/" + strconv.FormatInt(id, 10)

	if r := e.do(http.MethodPatch, path, "tok-bob", "", map[string]any{"name": "hijack"}, true); r.Code != http.StatusNotFound || r.code() != "not_found" {
		t.Errorf("他人の名前変更 = %d %s", r.Code, r.Body)
	}
	if r := e.do(http.MethodDelete, path, "tok-bob", "", nil, true); r.Code != http.StatusNotFound {
		t.Errorf("他人の削除 = %d", r.Code)
	}
	missing := base + "/9999"
	missingResp := e.do(http.MethodDelete, missing, "tok-bob", "", nil, true)
	otherResp := e.do(http.MethodDelete, path, "tok-bob", "", nil, true)
	if missingResp.Body.String() != otherResp.Body.String() {
		t.Error("無いものと他人のものを応答で区別させない")
	}
	l, _ := e.passkeys.ListByUser(context.Background(), 1)
	if len(l) != 1 || l[0].Name != "alice's" {
		t.Errorf("他人の操作で変わった: %+v", l)
	}
	bobList := e.do(http.MethodGet, base, "tok-bob", "", nil, false)
	if items := bobList.json()["passkeys"].([]any); len(items) != 0 {
		t.Errorf("他人のものが一覧に出た: %v", items)
	}
}

func TestSecurityLogsHaveNoSecrets(t *testing.T) {
	e := newEnv(t)
	token, pub, _ := e.begin("tok-alice")
	credID := newCredID()
	cred := attest(t, pub, credID, testOrigin)
	fr := e.finish("tok-alice", token, "x", cred)
	id := int64(fr.json()["id"].(float64))
	e.do(http.MethodDelete, base+"/"+strconv.FormatInt(id, 10), "tok-alice", "", nil, true)

	out := e.logs.String()
	for _, want := range []string{"passkey_registered", "passkey_deleted"} {
		if !strings.Contains(out, want) {
			t.Errorf("ログに %s が無い:\n%s", want, out)
		}
	}
	resp := cred["response"].(map[string]any)
	for name, secret := range map[string]string{
		"credential_id":  b64.EncodeToString(credID),
		"challenge":      pub["challenge"].(string),
		"challengeToken": token,
		"attestation":    resp["attestationObject"].(string),
	} {
		if strings.Contains(out, secret) {
			t.Errorf("ログに %s が含まれる", name)
		}
	}
}

func TestRegisterBeginSetsNoCookieInProduction(t *testing.T) {
	w, _ := passkey.NewWebAuthn(passkey.Config{RPID: testRPID, RPDisplayName: "MyTechPulse", Origins: []string{testOrigin}})
	clk := &clock{now: time.Now()}
	sessions := memSessions{rows: map[[32]byte]auth.Session{
		auth.HashToken("t"): {User: auth.User{ID: 1, Username: "alice"}, ExpiresAt: clk.now.Add(time.Hour)},
	}}
	h, _ := app.New(config.Config{Environment: "production", CookieName: "__Host-mtp_session", CookieSecure: true, CORSOrigins: []string{testOrigin}},
		app.Dependencies{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Auth:   &auth.Service{Sessions: sessions, Clock: clk},
			Passkey: &passkey.Service{WebAuthn: w, Passkeys: newMemPasskeys(), Clock: clk,
				Challenges: &memChallenges{rows: map[[32]byte]*memChallenge{}}},
		})
	req := httptest.NewRequest(http.MethodPost, base+"/register/begin", nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-MTP-CSRF", "1")
	req.AddCookie(&http.Cookie{Name: "__Host-mtp_session", Value: "t"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", rec.Code, rec.Body)
	}
	// チャレンジはCookieで運ばない。本番でも Set-Cookie は一切付かず、トークンは本文にある
	if got := rec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("Set-Cookie = %v", got)
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if token, _ := body["challengeToken"].(string); token == "" {
		t.Fatalf("本文にチャレンジトークンが無い: %s", rec.Body)
	}
}
