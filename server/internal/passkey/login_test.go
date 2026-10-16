package passkey_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

const (
	loginBase       = base + "/login"
	loginPathPW     = "/api/v1/auth/login"
	loginPassword   = "synthetic-pass-12"
	loginClientIPHd = "CF-Connecting-IP"
	// AuthenticatorData のフラグ
	flagUP = 0x01
	flagUV = 0x04
	flagBE = 0x08
	flagBS = 0x10
)

// ---- メモリ上の利用者・セッション ----

type loginUsers struct {
	mu       sync.Mutex
	users    map[string]auth.UserWithPassword
	sessions map[[32]byte]auth.Session
}

func (m *loginUsers) FindByUsername(_ context.Context, name string) (auth.UserWithPassword, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[name]
	if !ok {
		return auth.UserWithPassword{}, auth.ErrNotFound
	}
	return u, nil
}

func (m *loginUsers) FindByID(_ context.Context, id int64) (auth.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == id {
			return u.User, nil
		}
	}
	return auth.User{}, auth.ErrNotFound
}

func (m *loginUsers) CreateWithInterestsAndSession(context.Context, string, string, auth.Role, []string, [32]byte, time.Time) (auth.User, error) {
	return auth.User{}, errors.New("not used")
}

func (m *loginUsers) Create(_ context.Context, token [32]byte, userID int64, exp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.ID == userID {
			m.sessions[token] = auth.Session{User: u.User, ExpiresAt: exp}
			return nil
		}
	}
	return auth.ErrNotFound
}

func (m *loginUsers) FindUser(_ context.Context, h [32]byte, now time.Time) (auth.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[h]
	if !ok || !s.ExpiresAt.After(now) {
		return auth.Session{}, auth.ErrNotFound
	}
	return s, nil
}

func (m *loginUsers) Delete(context.Context, [32]byte) error         { return nil }
func (m *loginUsers) DeleteExpired(context.Context, time.Time) error { return nil }

type loginPasswords struct{}

func (loginPasswords) Hash(pw string) (string, error) { return "h:" + pw, nil }
func (loginPasswords) Compare(hash, pw string) error {
	if hash == "h:"+pw {
		return nil
	}
	return auth.ErrInvalidCredentials
}

// ---- ソフトウェア認証器(ログイン用) ----

type authenticator struct {
	key    *ecdsa.PrivateKey
	credID []byte
	handle []byte
	// 登録されている同期の状態。assertion の既定のフラグに使う
	be, bs bool
}

// assertion は navigator.credentials.get() の結果を作るための部品。試験が項目を書き換えて、不正な応答を作る。
type assertion struct {
	credID     []byte
	handle     []byte
	signer     *ecdsa.PrivateKey
	challenge  string
	origin     string
	typ        string
	rpID       string
	flags      byte
	counter    uint32
	tamperSign bool
}

func (a *authenticator) assertion(pub map[string]any, counter uint32) *assertion {
	flags := byte(flagUP | flagUV)
	if a.be {
		flags |= flagBE
	}
	if a.bs {
		flags |= flagBS
	}
	return &assertion{
		credID: a.credID, handle: a.handle, signer: a.key, challenge: pub["challenge"].(string),
		origin: testOrigin, typ: "webauthn.get", rpID: testRPID, flags: flags, counter: counter,
	}
}

// json はブラウザが返す PublicKeyCredential のJSON表現を作る。
func (x *assertion) json(t *testing.T) map[string]any {
	t.Helper()
	clientData, err := json.Marshal(map[string]any{
		"type": x.typ, "challenge": x.challenge, "origin": x.origin, "crossOrigin": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	rpHash := sha256.Sum256([]byte(x.rpID))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, x.flags)
	authData = binary.BigEndian.AppendUint32(authData, x.counter)

	cdHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), cdHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, x.signer, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if x.tamperSign {
		sig[len(sig)-1] ^= 0xff
	}
	return map[string]any{
		"id": b64.EncodeToString(x.credID), "rawId": b64.EncodeToString(x.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"authenticatorData": b64.EncodeToString(authData),
			"signature":         b64.EncodeToString(sig),
			"userHandle":        b64.EncodeToString(x.handle),
		},
		"clientExtensionResults": map[string]any{},
	}
}

// ---- 試験環境 ----

type loginEnv struct {
	t     *testing.T
	h     http.Handler
	clock *clock
	pk    *memPasskeys
	ch    *memChallenges
	users *loginUsers
	logs  *bytes.Buffer
}

func newLoginEnv(t *testing.T) *loginEnv {
	t.Helper()
	return newLoginEnvWith(t, config.Config{
		Environment: "local", CookieName: "mtp_session", CORSOrigins: []string{testOrigin}, ClientIPHeader: loginClientIPHd,
	})
}

func newLoginEnvWith(t *testing.T, cfg config.Config) *loginEnv {
	t.Helper()
	clk := &clock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	w, err := passkey.NewWebAuthn(passkey.Config{RPID: testRPID, RPDisplayName: "MyTechPulse", Origins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	users := &loginUsers{
		users: map[string]auth.UserWithPassword{
			"alice": {User: auth.User{ID: 1, Username: "alice", Role: auth.RoleMember}, PasswordHash: "h:" + loginPassword},
			"bob":   {User: auth.User{ID: 2, Username: "bob", Role: auth.RoleMember}, PasswordHash: "h:" + loginPassword},
		},
		sessions: map[[32]byte]auth.Session{},
	}
	// 登録の API を使う試験用に、ログイン済みのセッションを用意する
	users.sessions[auth.HashToken("tok-alice")] = auth.Session{User: users.users["alice"].User, ExpiresAt: clk.now.Add(time.Hour)}
	limiter := auth.NewLoginLimiter(clk)
	pk := newMemPasskeys()
	ch := &memChallenges{rows: map[[32]byte]*memChallenge{}}
	authSvc := &auth.Service{
		Users: users, Sessions: users, Passwords: loginPasswords{}, Tokens: auth.RandomTokenGenerator{}, Clock: clk,
		SessionTTL: 24 * time.Hour, DummyHash: "dummy", Logger: logger, Limiter: limiter,
	}
	svc := &passkey.Service{WebAuthn: w, Passkeys: pk, Challenges: ch, Clock: clk, Logger: logger, Limiter: limiter}
	h, _ := app.New(cfg, app.Dependencies{Logger: logger, Auth: authSvc, Passkey: svc})
	return &loginEnv{t: t, h: h, clock: clk, pk: pk, ch: ch, users: users, logs: logs}
}

// enroll はパスキーを直接保存した認証器を返す(登録APIは register_test.go で確かめる)。
func (e *loginEnv) enroll(userID int64, signCount uint32, be, bs bool) *authenticator {
	e.t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		e.t.Fatal(err)
	}
	fresh, err := passkey.NewUserHandle()
	if err != nil {
		e.t.Fatal(err)
	}
	handle, err := e.pk.UserHandle(context.Background(), userID, fresh)
	if err != nil {
		e.t.Fatal(err)
	}
	a := &authenticator{key: key, credID: newCredID(), handle: handle, be: be, bs: bs}
	_, err = e.pk.Create(context.Background(), passkey.Credential{
		UserID: userID, CredentialID: a.credID, PublicKey: cosePublicKey(&key.PublicKey), SignCount: signCount,
		Transports: []string{"internal"}, AAGUID: make([]byte, 16), BackupEligible: be, BackupState: bs, Name: "test",
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return a
}

type reqOpts struct {
	challenge, session, ip, origin string
	noCSRF                         bool
}

func (e *loginEnv) post(path string, body any, o reqOpts) resp {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(http.MethodPost, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if o.origin == "" {
		o.origin = testOrigin
	}
	req.Header.Set("Origin", o.origin)
	if !o.noCSRF {
		req.Header.Set("X-MTP-CSRF", "1")
	}
	if o.ip != "" {
		req.Header.Set(loginClientIPHd, o.ip)
	}
	if o.session != "" {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: o.session})
	}
	if o.challenge != "" {
		req.AddCookie(&http.Cookie{Name: "mtp_passkey_challenge", Value: o.challenge})
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

func (e *loginEnv) me(session string) resp {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "mtp_session", Value: session})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

// begin はログインを開始し、チャレンジCookieの値と端末へ渡す設定を返す。
func (e *loginEnv) begin(o reqOpts) (string, map[string]any, resp) {
	e.t.Helper()
	r := e.post(loginBase+"/begin", nil, o)
	if r.Code != http.StatusOK {
		return "", nil, r
	}
	c := r.challengeCookie()
	if c == nil || c.Value == "" {
		e.t.Fatal("チャレンジCookieが無い")
	}
	pub, _ := r.json()["publicKey"].(map[string]any)
	return c.Value, pub, r
}

func (e *loginEnv) finish(o reqOpts, cred map[string]any) resp {
	e.t.Helper()
	return e.post(loginBase+"/finish", map[string]any{"credential": cred}, o)
}

// signIn は begin → 署名 → finish を通す。counter は認証器が返す認証回数。
func (e *loginEnv) signIn(a *authenticator, counter uint32, o reqOpts) resp {
	e.t.Helper()
	token, pub, r := e.begin(o)
	if r.Code != http.StatusOK {
		e.t.Fatalf("begin = %d %s", r.Code, r.Body)
	}
	o.challenge = token
	return e.finish(o, a.assertion(pub, counter).json(e.t))
}

func sessionCookieOf(r resp) *http.Cookie {
	for _, c := range r.Result().Cookies() {
		if c.Name == "mtp_session" {
			return c
		}
	}
	return nil
}

func (e *loginEnv) wantLoginFailure(r resp, what string) {
	e.t.Helper()
	if r.Code != http.StatusUnauthorized || r.code() != "invalid_credentials" {
		e.t.Fatalf("%s = %d %s", what, r.Code, r.Body)
	}
	if sessionCookieOf(r) != nil {
		e.t.Errorf("%s: 失敗でセッションを発行してはいけない", what)
	}
}

// ---- 正常系 ----

func TestLoginSucceedsAndMatchesPasswordLoginSession(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)

	token, pub, br := e.begin(reqOpts{})
	if br.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", br.Code, br.Body)
	}
	if br.Header().Get("Cache-Control") != "no-store" {
		t.Error("beginの応答はキャッシュさせない")
	}
	if c := br.challengeCookie(); !c.HttpOnly || c.MaxAge != 300 || c.Path != "/" || c.SameSite != http.SameSiteStrictMode {
		t.Errorf("チャレンジCookieの属性が想定と違う: %+v", c)
	}
	if pub["rpId"] != testRPID || pub["userVerification"] != "required" {
		t.Errorf("publicKey = %v", pub)
	}
	if ac, _ := pub["allowCredentials"].([]any); len(ac) != 0 {
		t.Errorf("利用者名なしなので allowCredentials は空: %v", ac)
	}
	if token == pub["challenge"] {
		t.Error("Cookieの値とWebAuthnのチャレンジは別物")
	}

	fr := e.finish(reqOpts{challenge: token}, a.assertion(pub, 6).json(t))
	if fr.Code != http.StatusOK {
		t.Fatalf("finish = %d %s", fr.Code, fr.Body)
	}
	if fr.Header().Get("Cache-Control") != "no-store" {
		t.Error("finishの応答はキャッシュさせない")
	}
	user, _ := fr.json()["user"].(map[string]any)
	if user["username"] != "alice" || user["id"] != float64(1) || user["role"] != "member" {
		t.Errorf("user = %v", user)
	}
	if got := fr.challengeCookie(); got == nil || got.Value != "" || got.MaxAge >= 0 {
		t.Errorf("完了時にチャレンジCookieを破棄する: %+v", got)
	}

	// セッションCookieは、パスワードログインと同じ属性で発行される
	sc := sessionCookieOf(fr)
	if sc == nil || sc.Value == "" {
		t.Fatalf("セッションCookieが無い: %v", fr.Header().Values("Set-Cookie"))
	}
	pw := e.post(loginPathPW, map[string]any{"username": "alice", "password": loginPassword}, reqOpts{ip: "198.51.100.20"})
	pc := sessionCookieOf(pw)
	if pw.Code != http.StatusOK || pc == nil {
		t.Fatalf("パスワードログイン = %d %s", pw.Code, pw.Body)
	}
	if sc.Name != pc.Name || sc.Path != pc.Path || sc.Domain != pc.Domain || sc.HttpOnly != pc.HttpOnly ||
		sc.Secure != pc.Secure || sc.SameSite != pc.SameSite || sc.MaxAge != pc.MaxAge || !sc.Expires.Equal(pc.Expires) {
		t.Errorf("パスキー %+v とパスワード %+v のセッションCookieの属性が違う", sc, pc)
	}
	if !sc.HttpOnly || sc.SameSite != http.SameSiteLaxMode || sc.MaxAge != 86400 || sc.Path != "/" {
		t.Errorf("セッションCookieの属性 = %+v", sc)
	}
	if sc.Value == pc.Value {
		t.Error("セッションは毎回新しいトークンにする")
	}

	// 発行されたセッションで本人確認できる
	if r := e.me(sc.Value); r.Code != http.StatusOK || r.json()["username"] != "alice" {
		t.Errorf("/auth/me = %d %s", r.Code, r.Body)
	}

	// 使った記録
	stored := e.pk.byCredentialID(t, a.credID)
	if stored.SignCount != 6 || stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(e.clock.now) {
		t.Errorf("sign_count と last_used_at が更新されていない: %+v", stored)
	}
	if !stored.BackupEligible || stored.BackupState {
		t.Errorf("BE はそのまま、BS は変わらないはず: %+v", stored)
	}
}

func TestLoginPicksTheUserFromTheCredential(t *testing.T) {
	e := newLoginEnv(t)
	e.enroll(1, 1, true, false)
	b := e.enroll(2, 1, true, false)
	r := e.signIn(b, 2, reqOpts{})
	if r.Code != http.StatusOK {
		t.Fatalf("finish = %d %s", r.Code, r.Body)
	}
	if me := e.me(sessionCookieOf(r).Value); me.json()["username"] != "bob" {
		t.Errorf("bobのパスキーでbobとしてログインするはず: %s", me.Body)
	}
}

func TestLoginUpdatesBackupState(t *testing.T) {
	for _, c := range []struct {
		name         string
		storedBS     bool
		flagBS       bool
		wantBackedUp bool
	}{
		{"同期されていない → された", false, true, true},
		{"同期されていた → されなくなった", true, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLoginEnv(t)
			a := e.enroll(1, 5, true, c.storedBS)
			token, pub, _ := e.begin(reqOpts{})
			x := a.assertion(pub, 6)
			if c.flagBS {
				x.flags |= flagBS
			} else {
				x.flags &^= flagBS
			}
			if r := e.finish(reqOpts{challenge: token}, x.json(t)); r.Code != http.StatusOK {
				t.Fatalf("finish = %d %s", r.Code, r.Body)
			}
			if got := e.pk.byCredentialID(t, a.credID).BackupState; got != c.wantBackedUp {
				t.Errorf("backup_state = %v, want %v", got, c.wantBackedUp)
			}
		})
	}
}

// ---- 認証回数 ----

func TestLoginSignCount(t *testing.T) {
	for _, c := range []struct {
		name         string
		stored, recv uint32
		ok           bool
	}{
		{"保存5 → 受信3(巻き戻り)", 5, 3, false},
		{"保存5 → 受信5(増えない)", 5, 5, false},
		{"保存5 → 受信0(0へ戻る)", 5, 0, false},
		{"保存0 → 受信0(同期パスキー)", 0, 0, true},
		{"保存0 → 受信1", 0, 1, true},
		{"保存5 → 受信6", 5, 6, true},
		{"最大値へ", 4294967294, 4294967295, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLoginEnv(t)
			a := e.enroll(1, c.stored, true, false)
			r := e.signIn(a, c.recv, reqOpts{})
			stored := e.pk.byCredentialID(t, a.credID)
			logs := e.logs.String()
			if c.ok {
				if r.Code != http.StatusOK {
					t.Fatalf("finish = %d %s", r.Code, r.Body)
				}
				if stored.SignCount != c.recv || stored.LastUsedAt == nil || !stored.LastUsedAt.Equal(e.clock.now) {
					t.Errorf("更新されていない: %+v", stored)
				}
				if strings.Contains(logs, "passkey_sign_count_rollback") {
					t.Errorf("成功でセキュリティ事象を記録してはいけない:\n%s", logs)
				}
				return
			}
			e.wantLoginFailure(r, "巻き戻り")
			if stored.SignCount != c.stored || stored.LastUsedAt != nil {
				t.Errorf("拒否したら保存値は変えない: %+v", stored)
			}
			if !strings.Contains(logs, "passkey_sign_count_rollback") {
				t.Errorf("巻き戻りを記録していない:\n%s", logs)
			}
		})
	}
}

// ---- 失敗は区別できない ----

// failureCase は、finish が同一の401になるべき失敗を作る。
type failureCase struct {
	name string
	run  func(t *testing.T, e *loginEnv) resp
}

// prepared は alice のパスキー(保存の認証回数5、BEあり)を用意して、ログインを開始する。
func prepared(t *testing.T, e *loginEnv) (*authenticator, string, map[string]any) {
	t.Helper()
	a := e.enroll(1, 5, true, false)
	token, pub, r := e.begin(reqOpts{})
	if r.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", r.Code, r.Body)
	}
	return a, token, pub
}

// mutated は assertion を書き換えて finish まで進める。
func mutated(change func(*assertion)) func(*testing.T, *loginEnv) resp {
	return func(t *testing.T, e *loginEnv) resp {
		a, token, pub := prepared(t, e)
		x := a.assertion(pub, 6)
		change(x)
		return e.finish(reqOpts{challenge: token}, x.json(t))
	}
}

func rawCredential(cred map[string]any) func(*testing.T, *loginEnv) resp {
	return func(t *testing.T, e *loginEnv) resp {
		_, token, _ := prepared(t, e)
		return e.finish(reqOpts{challenge: token}, cred)
	}
}

// directChallenge は DB に直接ログイン用のチャレンジを入れ、そのCookie値を返す。
func directChallenge(t *testing.T, e *loginEnv, userID *int64, sessionData []byte) string {
	t.Helper()
	token, id, err := passkey.NewChallengeToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.ch.Create(context.Background(), id, passkey.PurposeLogin, userID, sessionData, e.clock.now.Add(passkey.ChallengeTTL)); err != nil {
		t.Fatal(err)
	}
	return token
}

func failureCases() []failureCase {
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return []failureCase{
		{"チャレンジCookieなし", func(t *testing.T, e *loginEnv) resp {
			a, _, pub := prepared(t, e)
			return e.finish(reqOpts{}, a.assertion(pub, 6).json(t))
		}},
		{"偽のチャレンジCookie", func(t *testing.T, e *loginEnv) resp {
			a, _, pub := prepared(t, e)
			return e.finish(reqOpts{challenge: "forged-token"}, a.assertion(pub, 6).json(t))
		}},
		{"期限切れ", func(t *testing.T, e *loginEnv) resp {
			a, token, pub := prepared(t, e)
			e.clock.now = e.clock.now.Add(passkey.ChallengeTTL + time.Second)
			return e.finish(reqOpts{challenge: token}, a.assertion(pub, 6).json(t))
		}},
		{"成功の後の再利用", func(t *testing.T, e *loginEnv) resp {
			a, token, pub := prepared(t, e)
			if r := e.finish(reqOpts{challenge: token}, a.assertion(pub, 6).json(t)); r.Code != http.StatusOK {
				t.Fatalf("1回目 = %d %s", r.Code, r.Body)
			}
			return e.finish(reqOpts{challenge: token}, a.assertion(pub, 7).json(t))
		}},
		{"失敗の後の再利用", func(t *testing.T, e *loginEnv) resp {
			a, token, pub := prepared(t, e)
			bad := a.assertion(pub, 6)
			bad.tamperSign = true
			if r := e.finish(reqOpts{challenge: token}, bad.json(t)); r.Code != http.StatusUnauthorized {
				t.Fatalf("1回目 = %d %s", r.Code, r.Body)
			}
			return e.finish(reqOpts{challenge: token}, a.assertion(pub, 6).json(t))
		}},
		{"登録用のチャレンジをログインに使う", func(t *testing.T, e *loginEnv) resp {
			a := e.enroll(1, 5, true, false)
			rb := e.post(base+"/register/begin", nil, reqOpts{session: "tok-alice"})
			if rb.Code != http.StatusOK {
				t.Fatalf("register/begin = %d %s", rb.Code, rb.Body)
			}
			pub, _ := rb.json()["publicKey"].(map[string]any)
			return e.finish(reqOpts{challenge: rb.challengeCookie().Value}, a.assertion(pub, 6).json(t))
		}},
		{"未知の認証情報ID", func(t *testing.T, e *loginEnv) resp {
			a, token, pub := prepared(t, e)
			x := a.assertion(pub, 6)
			x.credID = newCredID()
			return e.finish(reqOpts{challenge: token}, x.json(t))
		}},
		{"別の利用者のuserHandleへ差し替え", func(t *testing.T, e *loginEnv) resp {
			b := e.enroll(2, 1, true, false)
			a, token, pub := prepared(t, e)
			x := a.assertion(pub, 6)
			x.handle = b.handle
			return e.finish(reqOpts{challenge: token}, x.json(t))
		}},
		{"userHandleが空", mutated(func(x *assertion) { x.handle = []byte{} })},
		{"未知のuserHandle", mutated(func(x *assertion) { x.handle = bytes.Repeat([]byte{9}, 32) })},
		{"署名の改ざん", mutated(func(x *assertion) { x.tamperSign = true })},
		{"別の鍵での署名", mutated(func(x *assertion) { x.signer = otherKey })},
		{"オリジン不一致", mutated(func(x *assertion) { x.origin = "https://evil.example" })},
		{"typeがwebauthn.create", mutated(func(x *assertion) { x.typ = "webauthn.create" })},
		{"clientDataのチャレンジ不一致", mutated(func(x *assertion) {
			x.challenge = b64.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
		})},
		{"rpIdHashが別のRP", mutated(func(x *assertion) { x.rpID = "evil.example" })},
		{"本人確認(UV)なし", mutated(func(x *assertion) { x.flags &^= flagUV })},
		{"ユーザー確認(UP)なし", mutated(func(x *assertion) { x.flags &^= flagUP })},
		{"認証回数が巻き戻った(5→3)", mutated(func(x *assertion) { x.counter = 3 })},
		{"認証回数が増えない(5→5)", mutated(func(x *assertion) { x.counter = 5 })},
		{"認証回数が0へ戻った(5→0)", mutated(func(x *assertion) { x.counter = 0 })},
		{"BEが登録時から変わった(1→0)", mutated(func(x *assertion) { x.flags &^= flagBE })},
		{"BEが登録時から変わった(0→1)", func(t *testing.T, e *loginEnv) resp {
			a := e.enroll(1, 5, false, false)
			token, pub, _ := e.begin(reqOpts{})
			x := a.assertion(pub, 6)
			x.flags |= flagBE
			return e.finish(reqOpts{challenge: token}, x.json(t))
		}},
		{"BE=0なのにBS=1", func(t *testing.T, e *loginEnv) resp {
			a := e.enroll(1, 5, false, false)
			token, pub, _ := e.begin(reqOpts{})
			x := a.assertion(pub, 6)
			x.flags |= flagBS
			return e.finish(reqOpts{challenge: token}, x.json(t))
		}},
		{"空のcredential", rawCredential(map[string]any{})},
		{"項目が足りないcredential", rawCredential(map[string]any{"id": "x", "type": "public-key"})},
		{"responseが文字列", rawCredential(map[string]any{"id": "AAAA", "rawId": "AAAA", "type": "public-key", "response": "x"})},
		{"credentialの型が違う項目", rawCredential(map[string]any{"id": 1, "rawId": []any{1}, "type": 7, "response": map[string]any{"signature": 3}})},
		{"検証の後で利用者が消えていた", func(t *testing.T, e *loginEnv) resp {
			a, token, pub := prepared(t, e)
			e.users.mu.Lock()
			delete(e.users.users, "alice")
			e.users.mu.Unlock()
			return e.finish(reqOpts{challenge: token}, a.assertion(pub, 6).json(t))
		}},
		{"チャレンジに利用者が結び付いている", func(t *testing.T, e *loginEnv) resp {
			a, _, pub := prepared(t, e)
			uid := int64(1)
			data, _ := json.Marshal(webauthn.SessionData{Challenge: pub["challenge"].(string), UserVerification: "required"})
			return e.finish(reqOpts{challenge: directChallenge(t, e, &uid, data)}, a.assertion(pub, 6).json(t))
		}},
		{"保存された設定に利用者IDがある", func(t *testing.T, e *loginEnv) resp {
			a, _, pub := prepared(t, e)
			data, _ := json.Marshal(webauthn.SessionData{
				Challenge: pub["challenge"].(string), UserID: a.handle, UserVerification: "required",
			})
			return e.finish(reqOpts{challenge: directChallenge(t, e, nil, data)}, a.assertion(pub, 6).json(t))
		}},
		{"保存された設定が壊れている", func(t *testing.T, e *loginEnv) resp {
			a, _, pub := prepared(t, e)
			return e.finish(reqOpts{challenge: directChallenge(t, e, nil, []byte("not json"))}, a.assertion(pub, 6).json(t))
		}},
	}
}

func TestLoginEveryFailureIsTheSameResponse(t *testing.T) {
	cases := failureCases()
	if len(cases) < 25 {
		t.Fatalf("失敗の種類が足りない: %d", len(cases))
	}
	var first []byte
	var firstName, firstType string
	for _, c := range cases {
		e := newLoginEnv(t)
		r := c.run(t, e)
		e.wantLoginFailure(r, c.name)
		body := r.Body.Bytes()
		if first == nil {
			first, firstName, firstType = append([]byte{}, body...), c.name, r.Header().Get("Content-Type")
			if !strings.Contains(string(first), "invalid_credentials") {
				t.Fatalf("本文に code が無い: %s", first)
			}
			continue
		}
		if !bytes.Equal(body, first) {
			t.Errorf("%q の本文が %q と違う:\n%s\n%s", c.name, firstName, body, first)
		}
		if got := r.Header().Get("Content-Type"); got != firstType {
			t.Errorf("%q のContent-Type = %q, want %q", c.name, got, firstType)
		}
		if len(r.Result().Cookies()) != 0 {
			t.Errorf("%q: 失敗の応答はCookieを設定しない: %v", c.name, r.Header().Values("Set-Cookie"))
		}
	}
}

func TestLoginFailureLeavesTheCredentialUntouched(t *testing.T) {
	e := newLoginEnv(t)
	a, token, pub := prepared(t, e)
	x := a.assertion(pub, 9)
	x.tamperSign = true
	e.wantLoginFailure(e.finish(reqOpts{challenge: token}, x.json(t)), "署名の改ざん")
	stored := e.pk.byCredentialID(t, a.credID)
	if stored.SignCount != 5 || stored.LastUsedAt != nil || stored.BackupState {
		t.Errorf("失敗で保存値が変わった: %+v", stored)
	}
	e.users.mu.Lock()
	defer e.users.mu.Unlock()
	if len(e.users.sessions) != 1 { // 用意しておいた tok-alice だけ
		t.Errorf("失敗でセッションが増えた: %d", len(e.users.sessions))
	}
}

// ---- 他の用途との取り違え ----

func TestLoginChallengeCannotFinishRegistration(t *testing.T) {
	e := newLoginEnv(t)
	token, pub, _ := e.begin(reqOpts{})
	cred := attest(t, pub, newCredID(), testOrigin)
	r := e.post(base+"/register/finish", map[string]any{"credential": cred}, reqOpts{session: "tok-alice", challenge: token})
	if r.Code != http.StatusBadRequest || r.code() != "passkey_challenge_invalid" {
		t.Fatalf("ログイン用のチャレンジで登録 = %d %s", r.Code, r.Body)
	}
	if e.pk.count(1) != 0 {
		t.Error("登録されてはいけない")
	}
}

// ---- 回数の制限 ----

func (e *loginEnv) forgedFinish(ip string) resp {
	e.t.Helper()
	return e.post(loginBase+"/finish", map[string]any{"credential": map[string]any{}}, reqOpts{ip: ip, challenge: "forged"})
}

func TestLoginRateLimitLocksOutTheIPAndSharesThePasswordBucket(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.5"
	token, pub, _ := e.begin(reqOpts{ip: ip})

	for i := 0; i < 20; i++ {
		e.wantLoginFailure(e.forgedFinish(ip), "失敗"+strconv.Itoa(i+1))
	}

	// 正しい応答でも、締め出し中は429。チャレンジは消費しない
	r := e.finish(reqOpts{ip: ip, challenge: token}, a.assertion(pub, 6).json(t))
	if r.Code != http.StatusTooManyRequests || r.code() != "too_many_requests" {
		t.Fatalf("finish = %d %s", r.Code, r.Body)
	}
	if got := r.Header().Get("Retry-After"); got != "900" {
		t.Errorf("Retry-After = %q, want 900", got)
	}
	e.ch.mu.Lock()
	row := e.ch.rows[passkey.HashChallengeToken(token)]
	e.ch.mu.Unlock()
	if row == nil || row.used {
		t.Error("締め出し中はチャレンジを消費しない")
	}
	if got := e.pk.byCredentialID(t, a.credID).SignCount; got != 5 {
		t.Errorf("締め出し中に更新された: %d", got)
	}

	// 開始も、パスワードのログインも、同じIPなら429
	if r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip}); r.Code != http.StatusTooManyRequests || r.Header().Get("Retry-After") == "" {
		t.Errorf("begin = %d %s", r.Code, r.Body)
	}
	if r := e.post(loginPathPW, map[string]any{"username": "alice", "password": loginPassword}, reqOpts{ip: ip}); r.Code != http.StatusTooManyRequests {
		t.Errorf("同じIPのパスワードログイン = %d %s", r.Code, r.Body)
	}

	// 別のIPには影響しない
	if r := e.signIn(a, 6, reqOpts{ip: "203.0.113.6"}); r.Code != http.StatusOK {
		t.Errorf("別のIP = %d %s", r.Code, r.Body)
	}

	// 窓が終われば、また使える
	e.clock.now = e.clock.now.Add(15 * time.Minute)
	if r := e.signIn(a, 7, reqOpts{ip: ip}); r.Code != http.StatusOK {
		t.Errorf("窓の後 = %d %s", r.Code, r.Body)
	}
}

func TestLoginPasswordFailuresCountTowardsPasskeyLockout(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.30"
	// 利用者名ごとの締め出し(5回)に当たらないよう、名前を変えて20回失敗する
	for i := 0; i < 20; i++ {
		r := e.post(loginPathPW, map[string]any{"username": "nobody-" + strconv.Itoa(i), "password": "wrong-password-1"}, reqOpts{ip: ip})
		if r.Code != http.StatusUnauthorized {
			t.Fatalf("パスワード失敗%d = %d %s", i+1, r.Code, r.Body)
		}
	}
	if r := e.signIn(a, 6, reqOpts{ip: "203.0.113.31"}); r.Code != http.StatusOK {
		t.Fatalf("別のIP = %d", r.Code)
	}
	if r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip}); r.Code != http.StatusTooManyRequests {
		t.Errorf("パスワードで失敗を重ねたIPは、パスキーも締め出される: %d %s", r.Code, r.Body)
	}
}

func TestLoginBeginIsNotCountedAsAFailure(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.40"
	for i := 0; i < 59; i++ { // 開始の上限(60回)の手前まで。最後の1回は signIn の begin
		if _, _, r := e.begin(reqOpts{ip: ip}); r.Code != http.StatusOK {
			t.Fatalf("begin %d = %d %s", i+1, r.Code, r.Body)
		}
	}
	if r := e.signIn(a, 6, reqOpts{ip: ip}); r.Code != http.StatusOK {
		t.Errorf("beginを重ねても、ログインはできる: %d %s", r.Code, r.Body)
	}
}

func TestLoginBeginIsLimitedPerIPWithItsOwnQuota(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.60"
	token, pub, _ := e.begin(reqOpts{ip: ip}) // 1回目
	for i := 1; i < 60; i++ {
		if _, _, r := e.begin(reqOpts{ip: ip}); r.Code != http.StatusOK {
			t.Fatalf("begin %d = %d %s", i+1, r.Code, r.Body)
		}
	}
	e.ch.mu.Lock()
	stored := len(e.ch.rows)
	e.ch.mu.Unlock()

	// 61回目は429。チャレンジを保存しない(DBへ書かない)
	e.clock.now = e.clock.now.Add(4 * time.Minute) // チャレンジの有効期限(5分)の内
	r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip})
	if r.Code != http.StatusTooManyRequests || r.code() != "too_many_requests" {
		t.Fatalf("61回目のbegin = %d %s", r.Code, r.Body)
	}
	if got := r.Header().Get("Retry-After"); got != "660" {
		t.Errorf("Retry-After = %q, want 660", got)
	}
	if r.challengeCookie() != nil {
		t.Error("429にチャレンジCookieを付けてはいけない")
	}
	e.ch.mu.Lock()
	after := len(e.ch.rows)
	e.ch.mu.Unlock()
	if after != stored {
		t.Errorf("上限超えでチャレンジを保存した: %d -> %d", stored, after)
	}
	// 上限を超えた呼び出しは数えない(続けても、回復の時刻は延びない)
	if r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip}); r.Header().Get("Retry-After") != "660" {
		t.Errorf("連続しても待ち時間は延びない: %q", r.Header().Get("Retry-After"))
	}

	// 別のIPは影響を受けない
	if _, _, r := e.begin(reqOpts{ip: "203.0.113.61"}); r.Code != http.StatusOK {
		t.Errorf("別のIPのbegin = %d %s", r.Code, r.Body)
	}
	// beginの枠を使い切っても、finishの失敗枠・パスワードログインは使える
	e.wantLoginFailure(e.forgedFinish(ip), "枠切れでもfinishは失敗として扱う(429にしない)")
	if r := e.post(loginPathPW, map[string]any{"username": "alice", "password": loginPassword}, reqOpts{ip: ip}); r.Code != http.StatusOK {
		t.Errorf("同じIPのパスワードログイン = %d %s", r.Code, r.Body)
	}
	if fr := e.finish(reqOpts{ip: ip, challenge: token}, a.assertion(pub, 6).json(t)); fr.Code != http.StatusOK {
		t.Fatalf("開始済みのログインは完了できる: %d %s", fr.Code, fr.Body)
	}
	// 成功しても開始の数は戻らない
	if r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip}); r.Code != http.StatusTooManyRequests {
		t.Errorf("成功しても開始の数は戻らない: %d %s", r.Code, r.Body)
	}

	// 窓(最初の開始から15分)が終われば、また開始できる
	e.clock.now = e.clock.now.Add(11 * time.Minute)
	if r := e.signIn(a, 7, reqOpts{ip: ip}); r.Code != http.StatusOK {
		t.Errorf("窓の後 = %d %s", r.Code, r.Body)
	}
}

func TestLoginFailuresDoNotConsumeTheBeginQuota(t *testing.T) {
	e := newLoginEnv(t)
	const ip = "203.0.113.70"
	for i := 0; i < 19; i++ { // 失敗の枠(20回)の手前まで
		e.wantLoginFailure(e.forgedFinish(ip), "失敗"+strconv.Itoa(i+1))
	}
	for i := 0; i < 60; i++ {
		if _, _, r := e.begin(reqOpts{ip: ip}); r.Code != http.StatusOK {
			t.Fatalf("失敗を重ねても開始の枠は減らない: begin %d = %d %s", i+1, r.Code, r.Body)
		}
	}
	if r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip}); r.Code != http.StatusTooManyRequests {
		t.Errorf("61回目のbegin = %d %s", r.Code, r.Body)
	}
	e.wantLoginFailure(e.forgedFinish(ip), "開始が枠切れでも、失敗の枠は別に残っている")
}

func TestLoginSuccessDoesNotForgetIPFailures(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.50"
	for i := 0; i < 19; i++ {
		e.wantLoginFailure(e.forgedFinish(ip), "失敗")
	}
	if r := e.signIn(a, 6, reqOpts{ip: ip}); r.Code != http.StatusOK {
		t.Fatalf("19回失敗した後の成功 = %d %s", r.Code, r.Body)
	}
	e.wantLoginFailure(e.forgedFinish(ip), "20回目の失敗") // 成功しても数えは戻らないので、これで20回
	if r := e.post(loginBase+"/begin", nil, reqOpts{ip: ip}); r.Code != http.StatusTooManyRequests {
		t.Errorf("成功しても失敗の数は戻らない: %d %s", r.Code, r.Body)
	}
}

func TestLoginDatabaseFailuresAreServerErrorsAndNotCounted(t *testing.T) {
	dbErr := errors.New("db down: SECRET-DETAIL")
	for _, c := range []struct {
		name   string
		inject func(*loginEnv, error)
	}{
		{"認証情報の検索", func(e *loginEnv, err error) { e.pk.findErr = err }},
		{"利用者ハンドルの検索", func(e *loginEnv, err error) { e.pk.handleErr = err }},
		{"チャレンジの消費", func(e *loginEnv, err error) { e.ch.consumeErr = err }},
		{"使用記録の更新", func(e *loginEnv, err error) { e.pk.recordErr = err }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLoginEnv(t)
			a := e.enroll(1, 5, true, false)
			const ip = "203.0.113.60"
			c.inject(e, dbErr)
			for i := 0; i < 25; i++ {
				token, pub, br := e.begin(reqOpts{ip: ip})
				if br.Code != http.StatusOK {
					t.Fatalf("begin %d = %d %s(障害は失敗として数えない)", i+1, br.Code, br.Body)
				}
				r := e.finish(reqOpts{ip: ip, challenge: token}, a.assertion(pub, 6).json(t))
				if r.Code != http.StatusInternalServerError || r.code() != "internal_error" {
					t.Fatalf("finish %d = %d %s", i+1, r.Code, r.Body)
				}
				if strings.Contains(r.Body.String(), "SECRET-DETAIL") || sessionCookieOf(r) != nil {
					t.Fatalf("500で内部の詳細やセッションを出した: %s", r.Body)
				}
			}
			c.inject(e, nil)
			if r := e.signIn(a, 6, reqOpts{ip: ip}); r.Code != http.StatusOK {
				t.Errorf("障害が直れば成功する(25回の500で締め出されない): %d %s", r.Code, r.Body)
			}
		})
	}
}

func TestLoginMalformedBodyIsRejectedByValidationAndNotCounted(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.70"
	for i := 0; i < 25; i++ {
		for name, body := range map[string]any{
			"credentialが文字列": map[string]any{"credential": "x"},
			"credentialが無い":  map[string]any{},
		} {
			if r := e.post(loginBase+"/finish", body, reqOpts{ip: ip, challenge: "forged"}); r.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s = %d %s", name, r.Code, r.Body)
			}
		}
	}
	if r := e.signIn(a, 6, reqOpts{ip: ip}); r.Code != http.StatusOK {
		t.Errorf("形の不正は失敗として数えない: %d %s", r.Code, r.Body)
	}
}

// ---- チャレンジの清掃 ----

func TestLoginBeginCleansUpExpiredChallenges(t *testing.T) {
	e := newLoginEnv(t)
	first, _, _ := e.begin(reqOpts{})
	e.ch.mu.Lock()
	_, firstExists := e.ch.rows[passkey.HashChallengeToken(first)]
	e.ch.mu.Unlock()
	if !firstExists {
		t.Fatal("beginで保存されていない")
	}
	e.clock.now = e.clock.now.Add(passkey.ChallengeTTL + time.Minute)
	second, _, _ := e.begin(reqOpts{})

	e.ch.mu.Lock()
	defer e.ch.mu.Unlock()
	if _, ok := e.ch.rows[passkey.HashChallengeToken(first)]; ok {
		t.Error("期限切れのチャレンジが残っている")
	}
	if row := e.ch.rows[passkey.HashChallengeToken(second)]; row == nil || row.purpose != passkey.PurposeLogin || row.userID != nil {
		t.Errorf("新しいチャレンジはログイン用で、利用者に結び付けない: %+v", row)
	}
	if len(e.ch.rows) != 1 {
		t.Errorf("チャレンジの行数 = %d, want 1", len(e.ch.rows))
	}
}

// ---- CSRF・Origin ----

func TestLoginRoutesRequireOriginAndCSRFHeader(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	token, pub, _ := e.begin(reqOpts{})
	cred := a.assertion(pub, 6).json(t)
	for name, o := range map[string]reqOpts{
		"CSRFヘッダーなし": {noCSRF: true},
		"許可外のOrigin": {origin: "https://evil.example"},
	} {
		if r := e.post(loginBase+"/begin", nil, o); r.Code != http.StatusForbidden {
			t.Errorf("begin %s = %d %s", name, r.Code, r.Body)
		}
		o.challenge = token
		if r := e.post(loginBase+"/finish", map[string]any{"credential": cred}, o); r.Code != http.StatusForbidden {
			t.Errorf("finish %s = %d %s", name, r.Code, r.Body)
		}
	}
	// 拒否された要求はチャレンジを消費しない
	if r := e.finish(reqOpts{challenge: token}, cred); r.Code != http.StatusOK {
		t.Errorf("正しい要求 = %d %s", r.Code, r.Body)
	}
}

// ---- ログ ----

func TestLoginLogsRecordEventsWithoutSecrets(t *testing.T) {
	e := newLoginEnv(t)
	a := e.enroll(1, 5, true, false)
	const ip = "203.0.113.77"

	token, pub, _ := e.begin(reqOpts{ip: ip})
	cred := a.assertion(pub, 6).json(t)
	ok := e.finish(reqOpts{ip: ip, challenge: token}, cred)
	if ok.Code != http.StatusOK {
		t.Fatalf("finish = %d %s", ok.Code, ok.Body)
	}
	session := sessionCookieOf(ok).Value

	// 失敗(巻き戻り・署名の改ざん・偽のチャレンジ)も記録させる
	token2, pub2, _ := e.begin(reqOpts{ip: ip})
	rollback := a.assertion(pub2, 3)
	rollbackCred := rollback.json(t)
	e.wantLoginFailure(e.finish(reqOpts{ip: ip, challenge: token2}, rollbackCred), "巻き戻り")
	token3, pub3, _ := e.begin(reqOpts{ip: ip})
	bad := a.assertion(pub3, 9)
	bad.tamperSign = true
	badCred := bad.json(t)
	e.wantLoginFailure(e.finish(reqOpts{ip: ip, challenge: token3}, badCred), "署名の改ざん")
	e.wantLoginFailure(e.finish(reqOpts{ip: ip, challenge: "forged-cookie-value"}, cred), "偽のチャレンジ")

	logs := e.logs.String()
	for _, want := range []string{"passkey_login_succeeded", "passkey_login_failed", "passkey_sign_count_rollback", "reason=sign_count", "reason=verification", "reason=challenge"} {
		if !strings.Contains(logs, want) {
			t.Errorf("ログに %s が無い:\n%s", want, logs)
		}
	}

	secrets := map[string]string{
		"認証情報ID":            b64.EncodeToString(a.credID),
		"認証情報ID(hex)":       hex.EncodeToString(a.credID),
		"利用者ハンドル":           b64.EncodeToString(a.handle),
		"公開鍵":               b64.EncodeToString(cosePublicKey(&a.key.PublicKey)),
		"公開鍵(hex)":          hex.EncodeToString(cosePublicKey(&a.key.PublicKey)),
		"WebAuthnのチャレンジ1":   pub["challenge"].(string),
		"WebAuthnのチャレンジ2":   pub2["challenge"].(string),
		"WebAuthnのチャレンジ3":   pub3["challenge"].(string),
		"チャレンジCookie1":      token,
		"チャレンジCookie2":      token2,
		"偽のチャレンジCookie":     "forged-cookie-value",
		"セッションCookie":       session,
		"接続元IP":             ip,
		"利用者名":              "alice",
		"署名":                cred["response"].(map[string]any)["signature"].(string),
		"改ざんした署名":           badCred["response"].(map[string]any)["signature"].(string),
		"clientDataJSON":    cred["response"].(map[string]any)["clientDataJSON"].(string),
		"authenticatorData": cred["response"].(map[string]any)["authenticatorData"].(string),
		"オリジン":              testOrigin,
	}
	for name, secret := range secrets {
		if strings.Contains(logs, secret) {
			t.Errorf("ログに %s が含まれる", name)
		}
	}
}

// ---- 開始の応答 ----

func TestLoginBeginStoreFailures(t *testing.T) {
	t.Run("チャレンジを保存できない", func(t *testing.T) {
		e := newPathsEnv(t)
		e.ch.errCreate = errors.New("db down: SECRET-DETAIL")
		r := e.do(http.MethodPost, loginBase+"/begin", "", "", nil, true)
		if r.Code != http.StatusInternalServerError || r.code() != "internal_error" || strings.Contains(r.Body.String(), "SECRET-DETAIL") {
			t.Fatalf("begin = %d %s", r.Code, r.Body)
		}
		if r.challengeCookie() != nil {
			t.Error("保存できなかったチャレンジのCookieを渡してはいけない")
		}
	})
	t.Run("清掃だけの失敗ではログインを止めない", func(t *testing.T) {
		e := newPathsEnv(t)
		e.ch.errDeleteExpired = errors.New("db down: SECRET-DETAIL")
		r := e.do(http.MethodPost, loginBase+"/begin", "", "", nil, true)
		if r.Code != http.StatusOK || r.challengeCookie() == nil {
			t.Fatalf("begin = %d %s", r.Code, r.Body)
		}
		logs := e.logs.String()
		if !strings.Contains(logs, "expired passkey challenge cleanup failed") || strings.Contains(logs, "SECRET-DETAIL") {
			t.Errorf("警告だけを記録する:\n%s", logs)
		}
		if len(e.ch.created) != 1 || e.ch.created[0].purpose != passkey.PurposeLogin || e.ch.created[0].userID != nil {
			t.Errorf("ログイン用で、利用者に結び付けない: %+v", e.ch.created)
		}
		if want := e.clock.now.Add(passkey.ChallengeTTL); !e.ch.created[0].expires.Equal(want) {
			t.Errorf("期限 = %v, want %v", e.ch.created[0].expires, want)
		}
	})
}

func TestLoginOperationsAreDocumentedAsPublicWithErrorResponses(t *testing.T) {
	_, spec := app.New(config.Config{Environment: "local", CookieName: "mtp_session"},
		app.Dependencies{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Auth: &auth.Service{}, Passkey: &passkey.Service{}})
	for path, wantStatuses := range map[string][]string{
		loginBase + "/begin":  {"200", "429"},
		loginBase + "/finish": {"200", "401", "429", "422"},
	} {
		op := spec.Paths[path].Post
		if op == nil {
			t.Fatalf("%s が登録されていない", path)
		}
		if len(op.Security) != 0 {
			t.Errorf("%s: ログイン前の窓口に認証を要求してはいけない: %v", path, op.Security)
		}
		for _, status := range wantStatuses {
			if op.Responses[status] == nil {
				t.Errorf("%s: %s の応答が文書に無い", path, status)
			}
		}
		if !strings.Contains(op.Description, "X-MTP-CSRF") {
			t.Errorf("%s: CSRFヘッダーの説明が無い", path)
		}
	}
}

func TestLoginBeginIssuesDistinctChallengesEachTime(t *testing.T) {
	e := newLoginEnv(t)
	t1, p1, _ := e.begin(reqOpts{})
	t2, p2, _ := e.begin(reqOpts{})
	if t1 == t2 || p1["challenge"] == p2["challenge"] || t1 == "" || p1["challenge"] == "" {
		t.Errorf("チャレンジは毎回新しくする: %q %q / %v %v", t1, t2, p1["challenge"], p2["challenge"])
	}
}

// cookieNamed は応答の Set-Cookie から、名前が一致するものを返す。
func cookieNamed(r resp, name string) *http.Cookie {
	for _, c := range r.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestLoginInProductionUsesHostPrefixedSecureCookies(t *testing.T) {
	const sessionName, challengeName = "__Host-mtp_session", "__Host-mtp_passkey_challenge"
	e := newLoginEnvWith(t, config.Config{
		Environment: "production", CookieName: sessionName, CookieSecure: true,
		CORSOrigins: []string{testOrigin}, ClientIPHeader: loginClientIPHd,
	})
	a := e.enroll(1, 5, true, false)
	do := func(path, challengeCookieName, challenge string, body any) resp {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req := httptest.NewRequest(http.MethodPost, path, rd)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("X-MTP-CSRF", "1")
		req.Header.Set(loginClientIPHd, "203.0.113.80")
		if challenge != "" {
			req.AddCookie(&http.Cookie{Name: challengeCookieName, Value: challenge})
		}
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return resp{rec}
	}

	br := do(loginBase+"/begin", "", "", nil)
	if br.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", br.Code, br.Body)
	}
	cc := cookieNamed(br, challengeName)
	if cc == nil || cc.Value == "" || !cc.Secure || !cc.HttpOnly || cc.Path != "/" || cc.Domain != "" {
		t.Fatalf("本番のチャレンジCookie = %+v (%v)", cc, br.Header().Values("Set-Cookie"))
	}
	if br.challengeCookie() != nil {
		t.Error("本番で __Host- の付かない名前のチャレンジCookieを出してはいけない")
	}
	pub, _ := br.json()["publicKey"].(map[string]any)
	cred := a.assertion(pub, 6).json(t)

	// __Host- の付かない名前のチャレンジCookieでは、完了できない
	if r := do(loginBase+"/finish", "mtp_passkey_challenge", cc.Value, map[string]any{"credential": cred}); r.Code != http.StatusUnauthorized {
		t.Fatalf("本番で素の名前のチャレンジCookie = %d %s", r.Code, r.Body)
	}
	// 上の失敗でもチャレンジは使い切られるので、開始からやり直す
	br = do(loginBase+"/begin", "", "", nil)
	cc = cookieNamed(br, challengeName)
	pub, _ = br.json()["publicKey"].(map[string]any)
	fr := do(loginBase+"/finish", challengeName, cc.Value, map[string]any{"credential": a.assertion(pub, 7).json(t)})
	if fr.Code != http.StatusOK {
		t.Fatalf("finish = %d %s", fr.Code, fr.Body)
	}

	if cookies := fr.Result().Cookies(); len(cookies) != 2 {
		t.Fatalf("Set-Cookie は2つ(セッションと、チャレンジの破棄): %v", fr.Header().Values("Set-Cookie"))
	}
	sc := cookieNamed(fr, sessionName)
	if sc == nil || sc.Value == "" || !sc.Secure || !sc.HttpOnly || sc.Path != "/" || sc.Domain != "" || sc.SameSite != http.SameSiteLaxMode || sc.MaxAge != 86400 {
		t.Fatalf("セッションCookie = %+v", sc)
	}
	dc := cookieNamed(fr, challengeName)
	if dc == nil || dc.Value != "" || dc.MaxAge >= 0 || !dc.Secure || !dc.HttpOnly || dc.Path != "/" || dc.Domain != "" {
		t.Errorf("破棄するチャレンジCookie = %+v", dc)
	}
	// 発行されたセッションで本人確認できる
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: sessionName, Value: sc.Value})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("/auth/me = %d %s", rec.Code, rec.Body)
	}
}
