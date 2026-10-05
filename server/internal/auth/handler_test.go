package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

type apiHarness struct {
	*harness
	mux http.Handler
	api huma.API
}

func newAPIHarness(cookieName string, secure bool) *apiHarness {
	httpx.UseProblemErrors()
	h := newHarness()
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("test", "1.0.0")
	cfg.CreateHooks = nil // app.New と同じく、応答本文へ $schema を足さない
	api := humago.New(mux, cfg)
	Handler{Service: h.svc, CookieName: cookieName, CookieSecure: secure}.Register(api)
	return &apiHarness{harness: h, mux: mux, api: api}
}

func (a *apiHarness) do(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, req)
	return rec
}

func setCookies(rec *httptest.ResponseRecorder) []*http.Cookie {
	return (&http.Response{Header: rec.Header()}).Cookies()
}

func onlyCookie(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	cs := setCookies(rec)
	if len(cs) != 1 {
		t.Fatalf("want exactly one Set-Cookie, got %d: %v", len(cs), rec.Header().Values("Set-Cookie"))
	}
	return cs[0]
}

const (
	signupBody = `{"username":"synthetic-user","password":"synthetic-pass","favorite_tags":["go","react"]}`
	loginBody  = `{"username":"synthetic-user","password":"synthetic-pass"}`
)

func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("invalid JSON %q: %v", rec.Body.String(), err)
	}
}

type problemBody struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
	Code   string `json:"code"`
	Errors []struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	} `json:"errors"`
}

func assertProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) problemBody {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("content type = %q", ct)
	}
	var p problemBody
	decode(t, rec, &p)
	if p.Status != status || p.Code != code || p.Type != "about:blank" || p.Title == "" {
		t.Fatalf("problem = %+v", p)
	}
	return p
}

func TestHandlerSignupSuccess(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	rec := a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if want := `{"user":{"id":1,"username":"synthetic-user","role":"member"}}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("body = %s", rec.Body.String())
	}
	c := onlyCookie(t, rec)
	if c.Name != "mtp_session" || c.Value == "" || c.Path != "/" || !c.HttpOnly || c.Secure ||
		c.SameSite != http.SameSiteLaxMode || c.MaxAge != 86400 || c.Domain != "" {
		t.Fatalf("cookie = %+v", c)
	}
	if want := a.clock.now.Add(24 * time.Hour); !c.Expires.Equal(want) {
		t.Fatalf("expires = %v, want %v", c.Expires, want)
	}
	// 本文にパスワード・ハッシュ・トークンを含めない
	body := rec.Body.String()
	for _, secret := range []string{"synthetic-pass", "hash", c.Value, "password", "token"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response body must not contain %q: %s", secret, body)
		}
	}
	// 登録直後からログイン状態
	me := a.do(http.MethodGet, "/api/v1/auth/me", "", c)
	if me.Code != http.StatusOK {
		t.Fatalf("me after signup = %d", me.Code)
	}
}

func TestHandlerProductionCookieAttributes(t *testing.T) {
	a := newAPIHarness("__Host-mtp_session", true)
	c := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))
	if c.Name != "__Host-mtp_session" || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("production cookie = %+v", c)
	}
	// 本番名のCookieでも認証できる
	if rec := a.do(http.MethodGet, "/api/v1/auth/me", "", c); rec.Code != http.StatusOK {
		t.Fatalf("me = %d", rec.Code)
	}
	// local用の名前のCookieは認証に使えない
	other := &http.Cookie{Name: "mtp_session", Value: c.Value}
	assertProblem(t, a.do(http.MethodGet, "/api/v1/auth/me", "", other), http.StatusUnauthorized, "unauthenticated")
}

func TestHandlerSignupConflictAndValidation(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	if rec := a.do(http.MethodPost, "/api/v1/auth/signup", signupBody); rec.Code != http.StatusCreated {
		t.Fatal(rec.Body.String())
	}
	rec := a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
	assertProblem(t, rec, http.StatusConflict, "username_taken")
	if len(setCookies(rec)) != 0 {
		t.Fatal("conflict must not set a cookie")
	}

	secret := strings.Repeat("p", 73)
	cases := map[string]string{
		"password over 72 bytes": `{"username":"u","password":"` + secret + `","favorite_tags":["go"]}`,
		"blank username":         `{"username":"   ","password":"pw","favorite_tags":["go"]}`,
		"no tags":                `{"username":"u","password":"pw","favorite_tags":[]}`,
		"tag too long":           `{"username":"u","password":"pw","favorite_tags":["` + strings.Repeat("t", 51) + `"]}`,
		"missing field":          `{"username":"u","favorite_tags":["go"]}`,
		"wrong type":             `{"username":5,"password":"pw","favorite_tags":["go"]}`,
		"unknown property":       `{"username":"u","password":"pw","favorite_tags":["go"],"role":"admin"}`,
		"empty password":         `{"username":"u","password":"","favorite_tags":["go"]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := a.do(http.MethodPost, "/api/v1/auth/signup", body)
			assertProblem(t, rec, http.StatusUnprocessableEntity, "validation_failed")
			if strings.Contains(rec.Body.String(), secret) {
				t.Fatal("password must not be echoed")
			}
			if len(setCookies(rec)) != 0 {
				t.Fatal("validation error must not set a cookie")
			}
		})
	}
	// 管理者権限を名乗っても member になる
	if got := a.store.createdRoles; len(got) != 1 || got[0] != RoleMember {
		t.Fatalf("roles = %v", got)
	}
}

func TestHandlerMalformedJSONDoesNotEchoInput(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	rec := a.do(http.MethodPost, "/api/v1/auth/login", `{"username":"u","password":secret-pw-value}`)
	assertProblem(t, rec, http.StatusBadRequest, "bad_request")
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("input must not be echoed: %s", rec.Body.String())
	}
}

func TestHandlerLoginSuccess(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)
	rec := a.do(http.MethodPost, "/api/v1/auth/login", loginBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got AuthResponse
	decode(t, rec, &got)
	if got.User != (User{ID: 1, Username: "synthetic-user", Role: RoleMember}) {
		t.Fatalf("user = %+v", got.User)
	}
	c := onlyCookie(t, rec)
	if c.Value == "" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" {
		t.Fatalf("cookie = %+v", c)
	}
}

func TestHandlerLoginFailuresAreIdentical(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	a.do(http.MethodPost, "/api/v1/auth/signup", signupBody)

	missing := a.do(http.MethodPost, "/api/v1/auth/login", `{"username":"nobody","password":"synthetic-pass"}`)
	wrong := a.do(http.MethodPost, "/api/v1/auth/login", `{"username":"synthetic-user","password":"wrong-password"}`)
	assertProblem(t, missing, http.StatusUnauthorized, "invalid_credentials")
	assertProblem(t, wrong, http.StatusUnauthorized, "invalid_credentials")
	if missing.Body.String() != wrong.Body.String() {
		t.Fatalf("bodies differ:\n%s\n%s", missing.Body.String(), wrong.Body.String())
	}
	if len(setCookies(missing)) != 0 || len(setCookies(wrong)) != 0 {
		t.Fatal("failed login must not set a cookie")
	}
	assertProblem(t, a.do(http.MethodPost, "/api/v1/auth/login", `{"username":"","password":"x"}`), http.StatusUnprocessableEntity, "validation_failed")
}

func TestHandlerMe(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	c := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))

	rec := a.do(http.MethodGet, "/api/v1/auth/me", "", c)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if want := `{"id":1,"username":"synthetic-user","role":"member"}`; strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("body = %s", rec.Body.String())
	}

	assertProblem(t, a.do(http.MethodGet, "/api/v1/auth/me", ""), http.StatusUnauthorized, "unauthenticated")
	assertProblem(t, a.do(http.MethodGet, "/api/v1/auth/me", "", &http.Cookie{Name: "mtp_session", Value: "bogus"}),
		http.StatusUnauthorized, "unauthenticated")

	a.clock.now = a.clock.now.Add(24 * time.Hour)
	assertProblem(t, a.do(http.MethodGet, "/api/v1/auth/me", "", c), http.StatusUnauthorized, "unauthenticated")
}

func TestHandlerLogout(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	c1 := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/signup", signupBody))
	c2 := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/login", loginBody))

	rec := a.do(http.MethodPost, "/api/v1/auth/logout", "", c1)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	del := onlyCookie(t, rec)
	if del.Name != "mtp_session" || del.Value != "" || del.Path != "/" || del.MaxAge >= 0 ||
		!del.Expires.Equal(time.Unix(0, 0)) || !del.HttpOnly || del.SameSite != http.SameSiteLaxMode {
		t.Fatalf("delete cookie = %+v (%v)", del, rec.Header().Values("Set-Cookie"))
	}
	assertProblem(t, a.do(http.MethodGet, "/api/v1/auth/me", "", c1), http.StatusUnauthorized, "unauthenticated")
	if rec := a.do(http.MethodGet, "/api/v1/auth/me", "", c2); rec.Code != http.StatusOK {
		t.Fatalf("other device session must survive, got %d", rec.Code)
	}

	// Cookieが無くても、二重でも、204で削除Cookieを返す
	for _, cookies := range [][]*http.Cookie{nil, {c1}, {{Name: "mtp_session", Value: "bogus"}}} {
		rec := a.do(http.MethodPost, "/api/v1/auth/logout", "", cookies...)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
		}
		if del := onlyCookie(t, rec); del.MaxAge >= 0 {
			t.Fatalf("delete cookie = %+v", del)
		}
	}
}

func TestHandlerProductionLogoutCookieIsSecure(t *testing.T) {
	a := newAPIHarness("__Host-mtp_session", true)
	del := onlyCookie(t, a.do(http.MethodPost, "/api/v1/auth/logout", ""))
	if del.Name != "__Host-mtp_session" || !del.Secure || del.Path != "/" || del.Domain != "" {
		t.Fatalf("delete cookie = %+v", del)
	}
}

func TestHandlerOpenAPIContract(t *testing.T) {
	a := newAPIHarness("mtp_session", false)
	raw, err := json.Marshal(a.api.OpenAPI())
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths      map[string]map[string]struct{ Responses map[string]json.RawMessage } `json:"paths"`
		Components struct {
			Schemas         map[string]struct{ Properties map[string]json.RawMessage } `json:"schemas"`
			SecuritySchemes map[string]struct {
				Type string `json:"type"`
				In   string `json:"in"`
				Name string `json:"name"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		method   string
		statuses []string
	}{
		"/api/v1/auth/signup": {"post", []string{"201", "409", "422"}},
		"/api/v1/auth/login":  {"post", []string{"200", "401", "422"}},
		"/api/v1/auth/me":     {"get", []string{"200", "401"}},
		"/api/v1/auth/logout": {"post", []string{"204"}},
	}
	for path, w := range want {
		op, ok := spec.Paths[path][w.method]
		if !ok {
			t.Fatalf("%s %s missing", w.method, path)
		}
		for _, s := range w.statuses {
			if _, ok := op.Responses[s]; !ok {
				t.Errorf("%s %s lacks response %s", w.method, path, s)
			}
		}
	}
	if len(spec.Components.Schemas["User"].Properties) != 3 {
		t.Fatalf("User schema must expose exactly id, username, role: %v", spec.Components.Schemas["User"].Properties)
	}
	for _, prop := range []string{"id", "username", "role"} {
		if _, ok := spec.Components.Schemas["User"].Properties[prop]; !ok {
			t.Fatalf("User lacks %s", prop)
		}
	}
	if _, ok := spec.Components.Schemas["AuthResponse"].Properties["user"]; !ok || len(spec.Components.Schemas["AuthResponse"].Properties) != 1 {
		t.Fatalf("AuthResponse must have only user: %v", spec.Components.Schemas["AuthResponse"].Properties)
	}
	if got := spec.Components.SecuritySchemes["cookieAuth"]; got.Type != "apiKey" || got.In != "cookie" || got.Name != "mtp_session" {
		t.Fatalf("cookieAuth = %+v", got)
	}
	if _, ok := spec.Components.Schemas["ErrorModel"].Properties["code"]; !ok {
		t.Fatal("Problem Details schema must include code")
	}
}
