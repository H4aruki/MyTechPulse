package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

// newCustomAPI は Handler の設定(接続元IPのヘッダー名など)を変えた API を組み立てる。
func newCustomAPI(h *harness, handler Handler) *apiHarness {
	httpx.UseProblemErrors()
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.CreateHooks = nil
	api := humago.New(mux, cfg)
	handler.Service = h.svc
	if handler.CookieName == "" {
		handler.CookieName = "mtp_session"
	}
	handler.Register(api)
	return &apiHarness{harness: h, mux: mux, api: api}
}

func (a *apiHarness) doWithHeaders(method, path, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	return a.doFrom("", method, path, body, headers, cookies...)
}

// doFrom は接続元アドレス(req.RemoteAddr)を指定して要求する。空なら httptest の既定値のまま。
func (a *apiHarness) doFrom(remoteAddr, method, path, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	return rec
}

func loginJSON(user, password string) string {
	return fmt.Sprintf(`{"username":%q,"password":%q}`, user, password)
}

func assertInternalError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	p := assertProblem(t, rec, http.StatusInternalServerError, "internal_error")
	// 原因・DBの文言・入力値は応答へ出さない
	for _, leak := range []string{"secret-detail", "connection refused", "db:", "synthetic-user", "synthetic-pass"} {
		if strings.Contains(rec.Body.String(), leak) || strings.Contains(p.Detail, leak) {
			t.Fatalf("response must not contain %q: %s", leak, rec.Body.String())
		}
	}
	if len(setCookies(rec)) != 0 {
		t.Fatalf("an internal error must not set a cookie: %v", rec.Header().Values("Set-Cookie"))
	}
}

func faultAPI() (*apiHarness, *faultyStore) {
	h := newHarness()
	f := &faultyStore{fakeStore: h.store}
	h.svc.Users, h.svc.Sessions = f, f
	return newCustomAPI(h, Handler{}), f
}

func TestHandlerStoreFailuresBecomeGenericInternalErrors(t *testing.T) {
	t.Run("signup", func(t *testing.T) {
		a, f := faultAPI()
		f.createUserErr = errDB
		assertInternalError(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))
	})
	t.Run("login lookup", func(t *testing.T) {
		a, f := faultAPI()
		a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
		f.findByUsernameErr = errDB
		assertInternalError(t, a.do(http.MethodPost, "/api/v1/auth/login", loginBody))
	})
	t.Run("login session create", func(t *testing.T) {
		a, f := faultAPI()
		a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
		f.createSessionErr = errDB
		assertInternalError(t, a.do(http.MethodPost, "/api/v1/auth/login", loginBody))
	})
	t.Run("me is 500, not 401", func(t *testing.T) {
		a, f := faultAPI()
		c := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))
		f.findUserErr = errDB
		// 障害を「未ログイン」と見せると、画面が利用者を誤ってログアウト扱いにする
		assertInternalError(t, a.do(http.MethodGet, "/api/v1/auth/me", "", c))
	})
}

func TestHandlerLogoutRevocationFailureIsNotReportedAsSuccess(t *testing.T) {
	a, f := faultAPI()
	c := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))
	f.deleteErr = errDB

	assertInternalError(t, a.do(http.MethodPost, "/api/v1/auth/logout", "", c))

	// 失効できていないので、障害が直れば同じCookieで再試行でき、その後は無効になる
	f.deleteErr = nil
	if rec := a.do(http.MethodPost, "/api/v1/auth/logout", "", c); rec.Code != http.StatusNoContent {
		t.Fatalf("retry must succeed, got %d", rec.Code)
	}
	assertProblem(t, a.do(http.MethodGet, "/api/v1/auth/me", "", c), http.StatusUnauthorized, "unauthenticated")
}

func TestToProblemMapsEveryErrorKindWithoutLeaking(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		needLogin bool
		status    int
		code      string
	}{
		{"credentials on login", ErrInvalidCredentials, false, 401, "invalid_credentials"},
		{"no session on protected API", ErrInvalidCredentials, true, 401, "unauthenticated"},
		{"wrapped credentials", fmt.Errorf("x: %w", ErrInvalidCredentials), false, 401, "invalid_credentials"},
		{"username taken", ErrUsernameTaken, false, 409, "username_taken"},
		{"unknown error", errors.New("pq: secret-detail"), false, 500, "internal_error"},
		{"not found is not exposed", ErrNotFound, true, 500, "internal_error"},
		{"store error on protected API", errDB, true, 500, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toProblem(tc.err, tc.needLogin)
			p, ok := got.(*httpx.ErrorModel)
			if !ok {
				t.Fatalf("want *httpx.ErrorModel, got %T", got)
			}
			if p.Status != tc.status || p.Code != tc.code {
				t.Fatalf("problem = %d %s, want %d %s", p.Status, p.Code, tc.status, tc.code)
			}
			if strings.Contains(p.Detail, "secret-detail") || strings.Contains(p.Detail, "connection refused") {
				t.Fatalf("detail leaks the cause: %q", p.Detail)
			}
		})
	}
}

func TestHandlerRetryAfterRoundsUpToWholeSeconds(t *testing.T) {
	h := newHarness()
	h.svc.Limiter = NewLoginLimiter(h.clock)
	a := newCustomAPI(h, Handler{})
	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
	bad := loginJSON("synthetic-user", "wrong-password")
	for i := 0; i < 5; i++ {
		a.do(http.MethodPost, "/api/v1/auth/login", bad)
	}

	// 残り60秒 → 30.2秒 → 0.5秒。小数は切り上げ、0秒にはしない
	steps := []struct {
		advance time.Duration
		want    string
	}{
		{0, "60"},
		{29800 * time.Millisecond, "31"},
		{29700 * time.Millisecond, "1"},
	}
	for _, s := range steps {
		h.clock.now = h.clock.now.Add(s.advance)
		rec := a.do(http.MethodPost, "/api/v1/auth/login", loginBody)
		assertProblem(t, rec, http.StatusTooManyRequests, "too_many_requests")
		if got := rec.Header().Get("Retry-After"); got != s.want {
			t.Fatalf("Retry-After = %q, want %q", got, s.want)
		}
	}
	// 締め出しが明ければ入れる
	h.clock.now = h.clock.now.Add(500 * time.Millisecond)
	if rec := a.do(http.MethodPost, "/api/v1/auth/login", loginBody); rec.Code != http.StatusOK {
		t.Fatalf("after the lockout: %d %s", rec.Code, rec.Body.String())
	}
}

func TestToProblemRetryAfterNeverBelowOneSecond(t *testing.T) {
	for _, wait := range []time.Duration{0, time.Nanosecond, -time.Second} {
		err := toProblem(&TooManyAttemptsError{RetryAfter: wait}, false)
		he, ok := err.(huma.HeadersError)
		if !ok {
			t.Fatalf("429 must carry headers, got %T", err)
		}
		if got := he.GetHeaders().Get("Retry-After"); got != "1" {
			t.Fatalf("wait %v: Retry-After = %q, want 1", wait, got)
		}
	}
}

// 20回失敗したIPを締め出す準備: 別々の利用者名で失敗させる(利用者名ごとの上限に届かないように)。
func failFromIP(a *apiHarness, remoteAddr string, n int, headers map[string]string) {
	for i := 0; i < n; i++ {
		a.doFrom(remoteAddr, http.MethodPost, "/api/v1/auth/login", loginJSON(fmt.Sprintf("nobody-%d", i), "wrong-password"), headers)
	}
}

func TestHandlerClientIPHeaderSelectsTheLimiterKey(t *testing.T) {
	h := newHarness()
	h.svc.Limiter = NewLoginLimiter(h.clock)
	a := newCustomAPI(h, Handler{ClientIPHeader: "CF-Connecting-IP"})
	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)

	attacker := map[string]string{"CF-Connecting-IP": "203.0.113.9"}
	failFromIP(a, "", 20, attacker)

	// 同じ接続元(ヘッダーの値が同じ)は、正しいパスワードでも止まる
	rec := a.doWithHeaders(http.MethodPost, "/api/v1/auth/login", loginBody, map[string]string{"CF-Connecting-IP": " 203.0.113.9 "})
	assertProblem(t, rec, http.StatusTooManyRequests, "too_many_requests")
	if got := rec.Header().Get("Retry-After"); got != "900" {
		t.Fatalf("Retry-After = %q, want 900", got)
	}
	// 別の接続元は影響を受けない。テストの接続元アドレスは全要求で同じなので、
	// ヘッダーの値で分けていることの確認になる
	other := a.doWithHeaders(http.MethodPost, "/api/v1/auth/login", loginBody, map[string]string{"CF-Connecting-IP": "198.51.100.4"})
	if other.Code != http.StatusOK {
		t.Fatalf("other client ip: %d %s", other.Code, other.Body.String())
	}
}

func TestHandlerClientIPHeaderFallsBackToRemoteAddrWhenAbsentOrBlank(t *testing.T) {
	for name, headers := range map[string]map[string]string{
		"absent": nil,
		"blank":  {"CF-Connecting-IP": "   "},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			h.svc.Limiter = NewLoginLimiter(h.clock)
			a := newCustomAPI(h, Handler{ClientIPHeader: "CF-Connecting-IP"})
			a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)

			const attackerAddr = "198.51.100.7:4000"
			failFromIP(a, attackerAddr, 20, headers)

			// 同じ接続元アドレスは、ヘッダーが無い/空白だけでも接続元アドレスで数えられて止まる
			assertProblem(t, a.doFrom(attackerAddr, http.MethodPost, "/api/v1/auth/login", loginBody, headers),
				http.StatusTooManyRequests, "too_many_requests")
			// 別の接続元アドレスは影響を受けない(全員を1つの鍵にまとめていない)
			if rec := a.doFrom("198.51.100.8:4000", http.MethodPost, "/api/v1/auth/login", loginBody, headers); rec.Code != http.StatusOK {
				t.Fatalf("other remote addr: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlerIgnoresClientIPHeaderUnlessConfigured(t *testing.T) {
	h := newHarness()
	h.svc.Limiter = NewLoginLimiter(h.clock)
	a := newCustomAPI(h, Handler{}) // ClientIPHeader 未設定 = 信頼する中継なし
	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)

	// 攻撃者がヘッダーを毎回変えて、IPごとの制限を逃れようとする
	for i := 0; i < 20; i++ {
		spoof := map[string]string{"CF-Connecting-IP": fmt.Sprintf("203.0.113.%d", i), "X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i)}
		a.doWithHeaders(http.MethodPost, "/api/v1/auth/login", loginJSON(fmt.Sprintf("nobody-%d", i), "wrong-password"), spoof)
	}
	rec := a.doWithHeaders(http.MethodPost, "/api/v1/auth/login", loginBody, map[string]string{"CF-Connecting-IP": "192.0.2.200"})
	assertProblem(t, rec, http.StatusTooManyRequests, "too_many_requests")
}

func TestClientIPFallbacks(t *testing.T) {
	cases := []struct {
		name, header, remote, want string
	}{
		{"blank header uses remote host", "  \t", "10.0.0.2:5555", "10.0.0.2"},
		{"remote without port is returned as is", "", "10.0.0.2", "10.0.0.2"},
		{"unix-style remote is returned as is", "", "@", "@"},
		{"empty everything", "", "", ""},
		{"header wins even if remote is valid", "198.51.100.1", "10.0.0.2:5555", "198.51.100.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(tc.header, tc.remote); got != tc.want {
				t.Fatalf("clientIP(%q, %q) = %q, want %q", tc.header, tc.remote, got, tc.want)
			}
		})
	}
}
