package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

const webOrigin = "http://localhost:5173"

// メモリ上の利用者・セッション。HTTPの経路と共通処理の接続を確かめるためだけに使う。
type memStore struct {
	users    map[string]auth.UserWithPassword
	sessions map[[32]byte]auth.Session
}

func (m *memStore) FindByUsername(_ context.Context, name string) (auth.UserWithPassword, error) {
	u, ok := m.users[name]
	if !ok {
		return auth.UserWithPassword{}, auth.ErrNotFound
	}
	return u, nil
}

func (m *memStore) CreateWithInterestsAndSession(_ context.Context, name, hash string, role auth.Role,
	_ []string, token [32]byte, exp time.Time) (auth.User, error) {
	if _, ok := m.users[name]; ok {
		return auth.User{}, auth.ErrUsernameTaken
	}
	u := auth.User{ID: int64(len(m.users) + 1), Username: name, Role: role}
	m.users[name] = auth.UserWithPassword{User: u, PasswordHash: hash}
	m.sessions[token] = auth.Session{User: u, ExpiresAt: exp}
	return u, nil
}

func (m *memStore) Create(_ context.Context, token [32]byte, userID int64, exp time.Time) error {
	for _, u := range m.users {
		if u.ID == userID {
			m.sessions[token] = auth.Session{User: u.User, ExpiresAt: exp}
		}
	}
	return nil
}

func (m *memStore) FindUser(_ context.Context, token [32]byte, now time.Time) (auth.Session, error) {
	s, ok := m.sessions[token]
	if !ok || !s.ExpiresAt.After(now) {
		return auth.Session{}, auth.ErrNotFound
	}
	return s, nil
}

func (m *memStore) Delete(_ context.Context, token [32]byte) error {
	delete(m.sessions, token)
	return nil
}

func (m *memStore) DeleteExpired(context.Context, time.Time) error { return nil }

type plainPasswords struct{}

func (plainPasswords) Hash(pw string) (string, error) { return "h:" + pw, nil }
func (plainPasswords) Compare(hash, pw string) error {
	if hash == "h:"+pw {
		return nil
	}
	return auth.ErrInvalidCredentials
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }

func newAuthHandler() http.Handler {
	store := &memStore{users: map[string]auth.UserWithPassword{}, sessions: map[[32]byte]auth.Session{}}
	svc := auth.Service{
		Users: store, Sessions: store, Passwords: plainPasswords{}, Tokens: auth.RandomTokenGenerator{},
		Clock: fixedClock{}, SessionTTL: 24 * time.Hour, DummyHash: "dummy",
	}
	cfg := config.Config{
		Environment: "local", SwaggerEnabled: true, CORSOrigins: []string{webOrigin},
		CookieName: "mtp_session", SessionTTL: 24 * time.Hour,
	}
	h, _ := app.New(cfg, app.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Ready:  checker{},
		Auth:   &svc,
	})
	return h
}

func post(h http.Handler, path, body string, headers map[string]string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var csrfHeaders = map[string]string{"Origin": webOrigin, "X-MTP-CSRF": "1"}

const signupJSON = `{"username":"synthetic-user","password":"synthetic-pass","favorite_tags":["go"]}`

func TestAuthRoutesRejectStateChangesWithoutOriginOrCSRFHeader(t *testing.T) {
	h := newAuthHandler()
	for name, headers := range map[string]map[string]string{
		"no headers":     nil,
		"origin only":    {"Origin": webOrigin},
		"header only":    {"X-MTP-CSRF": "1"},
		"foreign origin": {"Origin": "https://evil.example", "X-MTP-CSRF": "1"},
	} {
		for _, path := range []string{"/api/v1/auth/signup", "/api/v1/auth/login", "/api/v1/auth/logout"} {
			rec := post(h, path, signupJSON, headers)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d", name, path, rec.Code)
			}
			if len(rec.Header().Values("Set-Cookie")) != 0 {
				t.Errorf("%s %s: must not set a cookie", name, path)
			}
			if rec.Header().Get("X-Request-ID") == "" {
				t.Errorf("%s %s: request id must be present even when rejected", name, path)
			}
		}
	}
}

func TestAuthRoutesEndToEnd(t *testing.T) {
	h := newAuthHandler()
	rec := post(h, "/api/v1/auth/signup", signupJSON, csrfHeaders)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != webOrigin || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("CORS headers missing: %v", rec.Header())
	}
	cookies := (&http.Response{Header: rec.Header()}).Cookies()
	if len(cookies) != 1 || cookies[0].Name != "mtp_session" {
		t.Fatalf("cookies = %v", cookies)
	}

	// GETはCSRFヘッダー不要
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.AddCookie(cookies[0])
	me := httptest.NewRecorder()
	h.ServeHTTP(me, req)
	if me.Code != http.StatusOK {
		t.Fatalf("me = %d", me.Code)
	}

	if out := post(h, "/api/v1/auth/logout", "", csrfHeaders, cookies[0]); out.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", out.Code)
	}
	me = httptest.NewRecorder()
	h.ServeHTTP(me, req)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d", me.Code)
	}
}

func TestPreflightForAuthRoutes(t *testing.T) {
	h := newAuthHandler()
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	ok := preflight(webOrigin)
	if ok.Code != http.StatusNoContent || ok.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("allowed preflight = %d %v", ok.Code, ok.Header())
	}
	if bad := preflight("https://evil.example"); bad.Code != http.StatusForbidden {
		t.Fatalf("unknown origin preflight = %d", bad.Code)
	}
}

func TestAuthRoutesAreAbsentWithoutAuthDependency(t *testing.T) {
	h := newHandler(testConfig("local", true), nil)
	if code := get(h, "/api/v1/auth/me").Code; code != http.StatusNotFound {
		t.Fatalf("status = %d", code)
	}
}
