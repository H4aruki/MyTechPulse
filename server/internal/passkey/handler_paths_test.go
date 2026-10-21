package passkey_test

// 失敗系・境界の経路。メモリ上の依存(register_test.go)に、失敗を注入できる包みを被せて確かめる。
// 時間は注入された時計で動かし、待たない。

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

// ---- 失敗を注入できる依存 ----

type faultPasskeys struct {
	*memPasskeys
	errUserHandle, errList, errRename, errDelete error
	// emptyHandle は、保存済みのハンドルが空で返ってくる壊れたDBを真似る。
	emptyHandle bool
	renameCalls []string
	deleteCalls int
}

func (f *faultPasskeys) UserHandle(ctx context.Context, userID int64, fresh []byte) ([]byte, error) {
	if f.errUserHandle != nil {
		return nil, f.errUserHandle
	}
	if f.emptyHandle {
		return nil, nil
	}
	return f.memPasskeys.UserHandle(ctx, userID, fresh)
}

func (f *faultPasskeys) ListByUser(ctx context.Context, userID int64) ([]passkey.Credential, error) {
	if f.errList != nil {
		return nil, f.errList
	}
	return f.memPasskeys.ListByUser(ctx, userID)
}

func (f *faultPasskeys) Rename(ctx context.Context, userID, id int64, name string) error {
	f.renameCalls = append(f.renameCalls, name)
	if f.errRename != nil {
		return f.errRename
	}
	return f.memPasskeys.Rename(ctx, userID, id, name)
}

func (f *faultPasskeys) Delete(ctx context.Context, userID, id int64) error {
	f.deleteCalls++
	if f.errDelete != nil {
		return f.errDelete
	}
	return f.memPasskeys.Delete(ctx, userID, id)
}

type createdChallenge struct {
	id      [32]byte
	purpose passkey.Purpose
	userID  *int64
	data    []byte
	expires time.Time
}

type faultChallenges struct {
	*memChallenges
	errCreate, errDeleteExpired error
	created                     []createdChallenge
	cleanedAt                   []time.Time
	consumeCalls                int
}

func (f *faultChallenges) Create(ctx context.Context, id [32]byte, p passkey.Purpose, userID *int64, data []byte, exp time.Time) error {
	if f.errCreate != nil {
		return f.errCreate
	}
	f.created = append(f.created, createdChallenge{id: id, purpose: p, userID: userID, data: data, expires: exp})
	return f.memChallenges.Create(ctx, id, p, userID, data, exp)
}

func (f *faultChallenges) Consume(ctx context.Context, id [32]byte, p passkey.Purpose, now time.Time) (passkey.Challenge, error) {
	f.consumeCalls++
	return f.memChallenges.Consume(ctx, id, p, now)
}

func (f *faultChallenges) DeleteExpired(ctx context.Context, now time.Time) error {
	f.cleanedAt = append(f.cleanedAt, now)
	if f.errDeleteExpired != nil {
		return f.errDeleteExpired
	}
	return f.memChallenges.DeleteExpired(ctx, now)
}

type flakySessions struct {
	memSessions
	err error
}

func (f *flakySessions) FindUser(ctx context.Context, h [32]byte, now time.Time) (auth.Session, error) {
	if f.err != nil {
		return auth.Session{}, f.err
	}
	return f.memSessions.FindUser(ctx, h, now)
}

type pathsEnv struct {
	*env
	svc  *passkey.Service
	pk   *faultPasskeys
	ch   *faultChallenges
	sess *flakySessions
	// appLogs は、アプリ本体(アクセスログ・異常終了の記録)のロガーの出力。env.logs はパスキーのService用。
	appLogs *bytes.Buffer
}

func newPathsEnv(t *testing.T) *pathsEnv {
	t.Helper()
	clk := &clock{now: time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)}
	logs, appLogs := &bytes.Buffer{}, &bytes.Buffer{}
	w, err := passkey.NewWebAuthn(passkey.Config{RPID: testRPID, RPDisplayName: "MyTechPulse", Origins: []string{testOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	pk := &faultPasskeys{memPasskeys: newMemPasskeys()}
	ch := &faultChallenges{memChallenges: &memChallenges{rows: map[[32]byte]*memChallenge{}}}
	svc := &passkey.Service{WebAuthn: w, Passkeys: pk, Challenges: ch, Clock: clk, Logger: slog.New(slog.NewTextHandler(logs, nil))}
	sess := &flakySessions{memSessions: memSessions{rows: map[[32]byte]auth.Session{}}}
	for tok, u := range map[string]auth.User{
		"tok-alice": {ID: 1, Username: "alice", Role: "member"},
		"tok-bob":   {ID: 2, Username: "bob", Role: "member"},
	} {
		sess.rows[auth.HashToken(tok)] = auth.Session{User: u, ExpiresAt: clk.now.Add(time.Hour)}
	}
	h, _ := app.New(config.Config{Environment: "local", CookieName: "mtp_session", CORSOrigins: []string{testOrigin}},
		app.Dependencies{Logger: slog.New(slog.NewTextHandler(appLogs, nil)), Auth: &auth.Service{Sessions: sess, Clock: clk}, Passkey: svc, Clock: &tickClock{now: clk.now}})
	return &pathsEnv{
		env: &env{t: t, h: h, clock: clk, passkeys: pk.memPasskeys, logs: logs},
		svc: svc, pk: pk, ch: ch, sess: sess, appLogs: appLogs,
	}
}

// rawDo は、Originやヘッダー・本文を自由に指定して送る。
func (e *env) rawDo(method, path, body string, headers map[string]string, cookies ...*http.Cookie) resp {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return resp{rec}
}

func alice() *http.Cookie { return &http.Cookie{Name: "mtp_session", Value: "tok-alice"} }

var jsonWrite = map[string]string{"Content-Type": "application/json", "Origin": testOrigin, "X-MTP-CSRF": "1"}

func (e *pathsEnv) passkeyPath(id int64) string { return base + "/" + strconv.FormatInt(id, 10) }

func (e *pathsEnv) nameOf(userID int64) []string {
	l, _ := e.passkeys.ListByUser(context.Background(), userID)
	var out []string
	for _, c := range l {
		out = append(out, c.Name)
	}
	return out
}

// ---- OpenAPI ----

func TestHandlerRegisterDocumentsCookieAuthOnEveryOperation(t *testing.T) {
	// 部品が空のOpenAPI文書でも、セッションCookieの方式を作って登録できる
	api := humago.New(http.NewServeMux(), huma.Config{OpenAPI: &huma.OpenAPI{OpenAPI: "3.1.0", Info: &huma.Info{Title: "t", Version: "1"}}})
	passkey.Handler{CookieName: "custom_session"}.Register(api)

	scheme := api.OpenAPI().Components.SecuritySchemes["cookieAuth"]
	if scheme == nil || scheme.Type != "apiKey" || scheme.In != "cookie" || scheme.Name != "custom_session" {
		t.Fatalf("scheme = %+v", scheme)
	}
	paths := api.OpenAPI().Paths
	ops := map[string]*huma.Operation{
		"begin":  paths[base+"/register/begin"].Post,
		"finish": paths[base+"/register/finish"].Post,
		"list":   paths[base].Get,
		"rename": paths[base+"/{id}"].Patch,
		"delete": paths[base+"/{id}"].Delete,
	}
	for name, op := range ops {
		if op == nil {
			t.Errorf("%s が登録されていない", name)
			continue
		}
		if len(op.Security) != 1 {
			t.Errorf("%s: 認証の要求が1つではない: %v", name, op.Security)
			continue
		}
		if _, ok := op.Security[0]["cookieAuth"]; !ok {
			t.Errorf("%s: Cookie認証を要求していない: %v", name, op.Security)
		}
	}
}

// ---- 認証 ----

// 目的: 注入した時計が認証(auth.Service)まで渡り、期限切れのセッションを全経路で拒否すること。
// 未ログイン・不正なトークンは TestRequiresLoginAndCSRF が確かめるので、ここは「期限切れ」だけを扱う。
func TestHandlerExpiredSessionIsUnauthorizedEverywhere(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	issued := len(e.ch.created)
	e.clock.now = e.clock.now.Add(2 * time.Hour) // セッションの期限(1時間)を過ぎる

	for _, c := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, base + "/register/begin", nil},
		{http.MethodPost, base + "/register/finish", map[string]any{"challengeToken": "t", "credential": map[string]any{}}},
		{http.MethodGet, base, nil},
		{http.MethodPatch, e.passkeyPath(id), map[string]any{"name": "taken-over"}},
		{http.MethodDelete, e.passkeyPath(id), nil},
	} {
		if r := e.do(c.method, c.path, "tok-alice", "", c.body, true); r.Code != http.StatusUnauthorized || r.code() != "unauthenticated" {
			t.Errorf("%s %s 期限切れのセッション = %d %s", c.method, c.path, r.Code, r.Body)
		}
	}
	if got := e.nameOf(1); len(got) != 1 || got[0] != "mine" {
		t.Errorf("期限切れのセッションで変わった: %v", got)
	}
	if len(e.ch.created) != issued {
		t.Error("ログインしていない要求でチャレンジを作ってはいけない")
	}
	if len(e.pk.renameCalls) != 0 {
		t.Errorf("認証前に名前変更の処理へ進んだ: %v", e.pk.renameCalls)
	}
}

func TestHandlerRejectsForeignOrMissingOrigin(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	issued := len(e.ch.created)

	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, base + "/register/begin", ""},
		{http.MethodPost, base + "/register/finish", `{"challengeToken":"t","credential":{}}`},
		{http.MethodPatch, e.passkeyPath(id), `{"name":"hijack"}`},
		{http.MethodDelete, e.passkeyPath(id), ""},
	} {
		for _, origin := range []string{"", "https://evil.example", "null", "*", testOrigin + ".evil.example"} {
			h := map[string]string{"Content-Type": "application/json", "X-MTP-CSRF": "1"}
			if origin != "" {
				h["Origin"] = origin
			}
			r := e.rawDo(c.method, c.path, c.body, h, alice())
			if r.Code != http.StatusForbidden || r.code() != "origin_not_allowed" {
				t.Errorf("%s %s Origin=%q = %d %s", c.method, c.path, origin, r.Code, r.Body)
			}
		}
	}
	if got := e.nameOf(1); len(got) != 1 || got[0] != "mine" {
		t.Errorf("他のOriginからの操作で変わった: %v", got)
	}
	if len(e.ch.created) != issued || len(e.pk.renameCalls) != 0 {
		t.Error("拒否した要求が処理まで進んだ")
	}
}

// ---- 依存の障害 ----

func TestHandlerStoreFailuresAreInternalErrorsThatHideTheCause(t *testing.T) {
	const leak = "pq: password authentication failed for user mtp_app host=10.0.0.5"
	boom := errors.New(leak)

	cases := []struct {
		name    string
		run     func(e *pathsEnv) resp
		noToken bool // チャレンジトークンを返してはいけない
	}{
		{"セッションの保存先", func(e *pathsEnv) resp {
			e.sess.err = boom
			return e.do(http.MethodGet, base, "tok-alice", "", nil, false)
		}, false},
		{"一覧", func(e *pathsEnv) resp {
			e.pk.errList = boom
			return e.do(http.MethodGet, base, "tok-alice", "", nil, false)
		}, false},
		{"名前変更", func(e *pathsEnv) resp {
			id, _ := e.register("tok-alice", "mine")
			e.pk.errRename = boom
			return e.do(http.MethodPatch, e.passkeyPath(id), "tok-alice", "", map[string]any{"name": "x"}, true)
		}, false},
		{"削除", func(e *pathsEnv) resp {
			id, _ := e.register("tok-alice", "mine")
			e.pk.errDelete = boom
			return e.do(http.MethodDelete, e.passkeyPath(id), "tok-alice", "", nil, true)
		}, false},
		{"登録開始: ハンドルの保存", func(e *pathsEnv) resp {
			e.pk.errUserHandle = boom
			return e.do(http.MethodPost, base+"/register/begin", "tok-alice", "", nil, true)
		}, true},
		{"登録開始: 既存の一覧", func(e *pathsEnv) resp {
			e.pk.errList = boom
			return e.do(http.MethodPost, base+"/register/begin", "tok-alice", "", nil, true)
		}, true},
		{"登録開始: チャレンジの保存", func(e *pathsEnv) resp {
			e.ch.errCreate = boom
			return e.do(http.MethodPost, base+"/register/begin", "tok-alice", "", nil, true)
		}, true},
		{"登録完了: 利用者の読み出し", func(e *pathsEnv) resp {
			token, pub, _ := e.begin("tok-alice")
			e.pk.errUserHandle = boom
			return e.finish("tok-alice", token, "", attest(e.t, pub, newCredID(), testOrigin))
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newPathsEnv(t)
			r := c.run(e)
			if r.Code != http.StatusInternalServerError || r.code() != "internal_error" {
				t.Fatalf("= %d %s", r.Code, r.Body)
			}
			if !strings.HasPrefix(r.Header().Get("Content-Type"), "application/problem+json") {
				t.Errorf("Content-Type = %q", r.Header().Get("Content-Type"))
			}
			for _, part := range []string{"pq:", "password", "10.0.0.5", "mtp_app"} {
				if strings.Contains(r.Body.String(), part) {
					t.Errorf("応答に原因が出ている(%q): %s", part, r.Body)
				}
			}
			if _, ok := r.json()["challengeToken"]; c.noToken && ok {
				t.Error("失敗したのにチャレンジトークンを返した")
			}
			r.noCookies(t, c.name)
			// パスキーのService用ロガーと、アプリ本体(アクセスログ等)のロガーの両方を調べる
			for name, out := range map[string]*bytes.Buffer{"パスキー": e.logs, "アプリ本体": e.appLogs} {
				if strings.Contains(out.String(), "10.0.0.5") {
					t.Errorf("%sのログに原因の詳細が出ている: %s", name, out)
				}
			}
		})
	}
}

// ---- 入力の形 ----

func TestHandlerMalformedIDsAreRejectedBeforeTouchingData(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	for _, id := range []string{"abc", "0", "-1", "1.5", "99999999999999999999", "1e3"} {
		for _, method := range []string{http.MethodPatch, http.MethodDelete} {
			body := ""
			if method == http.MethodPatch {
				body = `{"name":"x"}`
			}
			r := e.rawDo(method, base+"/"+id, body, jsonWrite, alice())
			if r.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s /%s = %d %s", method, id, r.Code, r.Body)
			}
		}
	}
	if got := e.nameOf(1); len(got) != 1 || got[0] != "mine" {
		t.Errorf("不正なIDで変わった: %v", got)
	}
	if len(e.pk.renameCalls) != 0 || e.pk.deleteCalls != 0 {
		t.Errorf("不正なIDで保存先へ進んだ: 名前変更=%v 削除=%d回", e.pk.renameCalls, e.pk.deleteCalls)
	}
	// 数えている呼び出し口が生きていること(正しいIDなら削除まで進む)
	if r := e.rawDo(http.MethodDelete, e.passkeyPath(id), "", jsonWrite, alice()); r.Code != http.StatusNoContent || e.pk.deleteCalls != 1 {
		t.Errorf("正しいIDの削除 = %d(削除の呼び出し %d回)", r.Code, e.pk.deleteCalls)
	}
}

func TestHandlerMalformedRenameBodiesAreRejected(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	path := e.passkeyPath(id)

	// 本文を解釈できないものは400 bad_request、解釈できて項目が不正なものは422 validation_failed
	for name, c := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"壊れたJSON":   {`{`, http.StatusBadRequest, "bad_request"},
		"空の本文":      {``, http.StatusBadRequest, "bad_request"},
		"配列":        {`[]`, http.StatusUnprocessableEntity, "validation_failed"},
		"文字列":       {`"x"`, http.StatusUnprocessableEntity, "validation_failed"},
		"nameが数値":   {`{"name":5}`, http.StatusUnprocessableEntity, "validation_failed"},
		"nameがnull": {`{"name":null}`, http.StatusUnprocessableEntity, "validation_failed"},
		"nameが無い":   {`{}`, http.StatusUnprocessableEntity, "validation_failed"},
		"nameが空":    {`{"name":""}`, http.StatusUnprocessableEntity, "validation_failed"},
		"nameが長すぎる": {`{"name":"` + strings.Repeat("x", 65) + `"}`, http.StatusUnprocessableEntity, "validation_failed"},
	} {
		r := e.rawDo(http.MethodPatch, path, c.body, jsonWrite, alice())
		if r.Code != c.status || r.code() != c.code {
			t.Errorf("%s = %d %s(期待 %d %s)", name, r.Code, r.Body, c.status, c.code)
		}
	}
	// JSON以外は受け付けない
	h := map[string]string{"Content-Type": "text/plain", "Origin": testOrigin, "X-MTP-CSRF": "1"}
	if r := e.rawDo(http.MethodPatch, path, `{"name":"x"}`, h, alice()); r.Code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain = %d %s", r.Code, r.Body)
	}
	// 入力した値は、不正の応答へ載せない
	secret := "SECRET-" + strings.Repeat("x", 70)
	r := e.rawDo(http.MethodPatch, path, `{"name":"`+secret+`"}`, jsonWrite, alice())
	if r.Code != http.StatusUnprocessableEntity || strings.Contains(r.Body.String(), "SECRET") {
		t.Errorf("長すぎる名前 = %d %s", r.Code, r.Body)
	}
	if got := e.nameOf(1); len(got) != 1 || got[0] != "mine" {
		t.Errorf("不正な入力で変わった: %v", got)
	}
	if len(e.pk.renameCalls) != 0 {
		t.Errorf("不正な入力で保存先へ進んだ: %v", e.pk.renameCalls)
	}
}

func TestHandlerRenameRejectsBlankNamesOfAnyKind(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	// 半角の空白以外(タブ・改行・全角の空白・改行なしの空白)だけの名前
	for _, blank := range []string{"\t", "\n", " \t\r\n ", " ", "  \t", "　", "　  "} {
		r := e.do(http.MethodPatch, e.passkeyPath(id), "tok-alice", "", map[string]any{"name": blank}, true)
		if r.Code != http.StatusUnprocessableEntity || r.code() != "validation_failed" {
			t.Errorf("名前 %q = %d %s", blank, r.Code, r.Body)
			continue
		}
		errs, _ := r.json()["errors"].([]any)
		if len(errs) != 1 || errs[0].(map[string]any)["field"] != "name" {
			t.Errorf("名前 %q: どの項目か分からない: %s", blank, r.Body)
		}
	}
	if got := e.nameOf(1); len(got) != 1 || got[0] != "mine" {
		t.Errorf("空白だけの名前で変わった: %v", got)
	}
	// 前後の空白(全角も)は除いて保存する
	if r := e.do(http.MethodPatch, e.passkeyPath(id), "tok-alice", "", map[string]any{"name": "　仕事用\t"}, true); r.Code != http.StatusNoContent {
		t.Fatalf("rename = %d %s", r.Code, r.Body)
	}
	if got := e.nameOf(1); got[0] != "仕事用" {
		t.Errorf("name = %q", got[0])
	}
}

// 目的: 制御文字・書式文字・見えない文字だけの名前は、名前変更で422(nameの項目エラー)になり、保存先を呼ばないこと。
func TestHandlerRenameRejectsControlAndInvisibleCharacters(t *testing.T) {
	e := newPathsEnv(t)
	id, _ := e.register("tok-alice", "mine")
	for name, bad := range map[string]string{
		"NUL":        "a\x00b",
		"改行":         "a\nb",
		"タブ":         "a\tb",
		"ESC":        "a\x1bb",
		"DEL":        "a\x7fb",
		"RLO":        "gpj.exe\U0000202Etxt",
		"ゼロ幅空白":      "a\U0000200Bb",
		"ゼロ幅空白だけ":    "\U0000200B",
		"空白+ゼロ幅空白":   " \U0000200B\U00003000",
		"文字に挟まれたZWJ": "a\U0000200Db",
		"行区切りだけ":     "\U00002028",
		"ハングルフィラーだけ": "\U00003164",
	} {
		r := e.do(http.MethodPatch, e.passkeyPath(id), "tok-alice", "", map[string]any{"name": bad}, true)
		if r.Code != http.StatusUnprocessableEntity || r.code() != "validation_failed" {
			t.Errorf("%s: 名前 %q = %d %s", name, bad, r.Code, r.Body)
			continue
		}
		errs, _ := r.json()["errors"].([]any)
		if len(errs) != 1 || errs[0].(map[string]any)["field"] != "name" {
			t.Errorf("%s: どの項目か分からない: %s", name, r.Body)
		}
	}
	if len(e.pk.renameCalls) != 0 {
		t.Errorf("不正な名前で保存先へ進んだ: %q", e.pk.renameCalls)
	}
	if got := e.nameOf(1); len(got) != 1 || got[0] != "mine" {
		t.Errorf("不正な名前で変わった: %v", got)
	}
	// 数えている呼び出し口が生きていること(絵文字の家族は、ZWJを含んでも保存まで進む)
	family := "\U0001F468\U0000200D\U0001F469\U0000200D\U0001F467"
	if r := e.do(http.MethodPatch, e.passkeyPath(id), "tok-alice", "", map[string]any{"name": family}, true); r.Code != http.StatusNoContent {
		t.Fatalf("絵文字の名前 = %d %s", r.Code, r.Body)
	}
	if len(e.pk.renameCalls) != 1 || e.pk.renameCalls[0] != family {
		t.Errorf("保存先の呼び出し = %q", e.pk.renameCalls)
	}
}

// 目的: 登録完了でも同じ名前の検査が効き、422(nameの項目エラー)になること。
// 保存せず、チャレンジも消費しない(入力ミスで手続きをやり直させない)。
func TestHandlerFinishRejectsControlCharactersInNameWithoutBurningTheChallenge(t *testing.T) {
	e := newPathsEnv(t)
	token, pub, _ := e.begin("tok-alice")
	for name, bad := range map[string]string{
		"NUL":     "a\x00b",
		"改行":      "a\nb",
		"RLO":     "gpj.exe\U0000202Etxt",
		"ゼロ幅空白だけ": "\U0000200B",
	} {
		r := e.finish("tok-alice", token, bad, attest(t, pub, newCredID(), testOrigin))
		if r.Code != http.StatusUnprocessableEntity || r.code() != "validation_failed" {
			t.Errorf("%s: 名前 %q = %d %s", name, bad, r.Code, r.Body)
			continue
		}
		errs, _ := r.json()["errors"].([]any)
		if len(errs) != 1 || errs[0].(map[string]any)["field"] != "name" {
			t.Errorf("%s: どの項目か分からない: %s", name, r.Body)
		}
	}
	if e.passkeys.count(1) != 0 {
		t.Fatal("不正な名前で登録された")
	}
	if e.ch.consumeCalls != 0 {
		t.Fatalf("不正な名前でチャレンジを消費した(%d回)", e.ch.consumeCalls)
	}
	// 同じチャレンジで、正しい名前の要求はまだ通る
	if r := e.finish("tok-alice", token, "iPhone", attest(t, pub, newCredID(), testOrigin)); r.Code != http.StatusCreated {
		t.Fatalf("やり直し = %d %s", r.Code, r.Body)
	}
}

func TestHandlerFinishRejectsBadRequestsWithoutBurningTheChallenge(t *testing.T) {
	e := newPathsEnv(t)
	token, pub, _ := e.begin("tok-alice")
	finishPath := base + "/register/finish"
	tok := `"challengeToken":"` + token + `"`

	// 形の不正は、チャレンジを消費する前に拒否する(入力ミスで手続きをやり直させない)
	for name, c := range map[string]struct {
		body   string
		status int
		code   string
	}{
		"壊れたJSON":        {`{`, http.StatusBadRequest, "bad_request"},
		"credentialが無い":  {`{` + tok + `,"name":"x"}`, http.StatusUnprocessableEntity, "validation_failed"},
		"credentialが文字列": {`{` + tok + `,"credential":"x"}`, http.StatusUnprocessableEntity, "validation_failed"},
		"名前が長すぎる":        {`{` + tok + `,"credential":{},"name":"` + strings.Repeat("あ", 65) + `"}`, http.StatusUnprocessableEntity, "validation_failed"},
		"名前が数値":          {`{` + tok + `,"credential":{},"name":5}`, http.StatusUnprocessableEntity, "validation_failed"},
		"トークンが無い":        {`{"credential":{}}`, http.StatusUnprocessableEntity, "validation_failed"},
		"トークンが空":         {`{"challengeToken":"","credential":{}}`, http.StatusUnprocessableEntity, "validation_failed"},
		"トークンが長すぎる":      {`{"challengeToken":"` + strings.Repeat("a", 129) + `","credential":{}}`, http.StatusUnprocessableEntity, "validation_failed"},
		"トークンが数値":        {`{"challengeToken":5,"credential":{}}`, http.StatusUnprocessableEntity, "validation_failed"},
	} {
		r := e.rawDo(http.MethodPost, finishPath, c.body, jsonWrite, alice())
		if r.Code != c.status || r.code() != c.code {
			t.Errorf("%s = %d %s(期待 %d %s)", name, r.Code, r.Body, c.status, c.code)
		}
	}
	if e.ch.consumeCalls != 0 {
		t.Fatalf("形が不正な要求でチャレンジを消費した(%d回)", e.ch.consumeCalls)
	}
	if e.passkeys.count(1) != 0 {
		t.Fatal("不正な要求で登録された")
	}
	// 同じチャレンジで、正しい要求はまだ通る
	r := e.finish("tok-alice", token, "iPhone", attest(t, pub, newCredID(), testOrigin))
	if r.Code != http.StatusCreated {
		t.Fatalf("やり直し = %d %s", r.Code, r.Body)
	}
}

// ---- 登録開始 ----

func TestHandlerBeginExcludesEveryOwnCredentialAndNoOthers(t *testing.T) {
	e := newPathsEnv(t)
	want := map[string]bool{}
	for i := 0; i < passkey.MaxPasskeysPerUser-1; i++ {
		_, id := e.register("tok-alice", "p"+strconv.Itoa(i))
		want[b64.EncodeToString(id)] = true
	}
	e.register("tok-bob", "bob") // 他人の認証情報は、want に無いので「想定外の除外」として検出される

	_, pub, r := e.begin("tok-alice")
	if r.Code != http.StatusOK {
		t.Fatalf("begin = %d %s", r.Code, r.Body)
	}
	ex, _ := pub["excludeCredentials"].([]any)
	if len(ex) != len(want) {
		t.Fatalf("excludeCredentials は %d 件のはずが %d 件", len(want), len(ex))
	}
	for _, item := range ex {
		m := item.(map[string]any)
		id, _ := m["id"].(string)
		if m["type"] != "public-key" || !want[id] {
			t.Errorf("想定外の除外: %v", m)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Errorf("除外されていない認証情報がある: %v", want)
	}
}

func TestHandlerBeginStoresOnlyTheHashAndExpiresInFiveMinutes(t *testing.T) {
	e := newPathsEnv(t)
	token, pub, r := e.begin("tok-alice")
	if r.Code != http.StatusOK {
		t.Fatalf("begin = %d", r.Code)
	}
	if len(e.ch.created) != 1 {
		t.Fatalf("チャレンジは1件のはずが %d 件", len(e.ch.created))
	}
	c := e.ch.created[0]
	if c.id != passkey.HashChallengeToken(token) {
		t.Error("保存するidは、トークンのSHA-256であること")
	}
	if c.purpose != passkey.PurposeRegister || c.userID == nil || *c.userID != 1 {
		t.Errorf("用途と利用者が違う: %v %v", c.purpose, c.userID)
	}
	if !c.expires.Equal(e.clock.now.Add(passkey.ChallengeTTL)) {
		t.Errorf("期限 = %v、現在 = %v", c.expires, e.clock.now)
	}
	// 手続きの中身(WebAuthnのセッション)に、トークンの平文を入れない
	if bytes.Contains(c.data, []byte(token)) {
		t.Error("保存するセッションデータにトークンの平文が入っている")
	}
	// 端末へ渡すチャレンジは、保存するセッションデータの中にある(完了時の照合に使う)
	if !bytes.Contains(c.data, []byte(pub["challenge"].(string))) {
		t.Error("セッションデータに端末へ渡したチャレンジが無い")
	}
	if len(e.ch.cleanedAt) != 1 || !e.ch.cleanedAt[0].Equal(e.clock.now) {
		t.Errorf("期限切れの清掃は注入された時計の「いま」で行う: %v", e.ch.cleanedAt)
	}
}
