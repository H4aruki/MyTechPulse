package app_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

type stepClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

const ipHeader = "CF-Connecting-IP"

func newLimitedHandler(clock *stepClock) http.Handler {
	store := &memStore{users: map[string]auth.UserWithPassword{}, sessions: map[[32]byte]auth.Session{}}
	svc := auth.Service{
		Users: store, Sessions: store, Passwords: plainPasswords{}, Tokens: auth.RandomTokenGenerator{},
		Clock: fixedClock{}, SessionTTL: 24 * time.Hour, DummyHash: "dummy",
	}
	cfg := config.Config{
		Environment: "test", CORSOrigins: []string{webOrigin}, CookieName: "mtp_session",
		SessionTTL: 24 * time.Hour, ClientIPHeader: ipHeader,
	}
	h, _ := app.New(cfg, app.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Ready:  checker{},
		Auth:   &svc,
		Clock:  clock,
	})
	return h
}

func newStepClock() *stepClock {
	return &stepClock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
}

func signupFrom(h http.Handler, ip string, n int) *httptest.ResponseRecorder {
	body := fmt.Sprintf(`{"username":"user-%s-%d","password":"synthetic-pass-12","favorite_tags":["go"]}`, ip, n)
	headers := map[string]string{"Origin": webOrigin, "X-MTP-CSRF": "1", ipHeader: ip}
	return post(h, "/api/v1/auth/signup", body, headers)
}

func TestSignupWithoutCSRFHeaderDoesNotUseTheQuota(t *testing.T) {
	h := newLimitedHandler(newStepClock())
	for i := 0; i < 30; i++ {
		rec := post(h, "/api/v1/auth/signup", signupJSON, map[string]string{ipHeader: "203.0.113.5"})
		if rec.Code != http.StatusForbidden {
			t.Fatalf("no csrf %d = %d", i, rec.Code)
		}
	}
	if rec := signupFrom(h, "203.0.113.5", 0); rec.Code != http.StatusCreated {
		t.Fatalf("signup = %d", rec.Code)
	}
}

func TestSignupRejectsInvisibleCharactersIn422(t *testing.T) {
	h := newLimitedHandler(newStepClock())
	headers := map[string]string{"Origin": webOrigin, "X-MTP-CSRF": "1", ipHeader: "203.0.113.20"}
	cases := map[string]string{
		"favorite_tags": `{"username":"u1","password":"synthetic-pass-12","favorite_tags":["go","a​b"]}`,
		"username":      `{"username":"u​1","password":"synthetic-pass-12","favorite_tags":["go"]}`,
	}
	for field, body := range cases {
		rec := post(h, "/api/v1/auth/signup", body, headers)
		if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"field":"`+field+`"`) {
			t.Errorf("%s: %d %s", field, rec.Code, rec.Body.String())
		}
	}
}

func TestGlobalIPLimitCountsAPIRequestsButNotHealth(t *testing.T) {
	clock := newStepClock()
	h := newLimitedHandler(clock)
	getFrom := func(path, ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set(ipHeader, ip)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	for i := 0; i < 300; i++ {
		if rec := getFrom("/api/v1/auth/me", "198.51.100.1"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("request %d = %d", i, rec.Code)
		}
	}
	rec := getFrom("/api/v1/auth/me", "198.51.100.1")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("301st = %d %v", rec.Code, rec.Header())
	}
	// ヘルスチェックは何度呼んでも制限しない(同じIPからでも)
	for i := 0; i < 400; i++ {
		if rec := getFrom("/health/live", "198.51.100.1"); rec.Code != http.StatusOK {
			t.Fatalf("health %d = %d", i, rec.Code)
		}
		if rec := getFrom("/health/ready", "198.51.100.1"); rec.Code != http.StatusOK {
			t.Fatalf("ready %d = %d", i, rec.Code)
		}
	}
	// 別のIPは影響を受けない
	if rec := getFrom("/api/v1/auth/me", "198.51.100.2"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("other IP = %d", rec.Code)
	}
	clock.Advance(time.Second)
	if rec := getFrom("/api/v1/auth/me", "198.51.100.1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after waiting = %d", rec.Code)
	}
}

func TestIPv6AddressesInTheSame64AreCountedTogether(t *testing.T) {
	h := newLimitedHandler(newStepClock())
	for i := 0; i < 20; i++ {
		if rec := signupFrom(h, fmt.Sprintf("2001:db8:1:2::%x", i+1), i); rec.Code != http.StatusCreated {
			t.Fatalf("signup %d = %d", i, rec.Code)
		}
	}
	if rec := signupFrom(h, "2001:db8:1:2:ffff::1", 9); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("same /64 = %d", rec.Code)
	}
	if rec := signupFrom(h, "2001:db8:1:3::1", 9); rec.Code != http.StatusCreated {
		t.Fatalf("other /64 = %d", rec.Code)
	}
}

func TestOversizedBodiesAreRejectedWith413(t *testing.T) {
	h := newLimitedHandler(newStepClock())
	filler := strings.Repeat("a", 64*1024)
	body := `{"username":"u","password":"synthetic-pass-12","favorite_tags":["go"],"x":"` + filler + `"}`
	headers := map[string]string{"Origin": webOrigin, "X-MTP-CSRF": "1"}

	rec := post(h, "/api/v1/auth/signup", body, headers)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "payload_too_large") {
		t.Fatalf("content-length body = %d: %.200s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q", ct)
	}

	// 長さを申告しない(チャンク)本文も同じ
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webOrigin)
	req.Header.Set("X-MTP-CSRF", "1")
	chunked := httptest.NewRecorder()
	h.ServeHTTP(chunked, req)
	if chunked.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked body = %d: %.200s", chunked.Code, chunked.Body.String())
	}

	// 上限ちょうど以下は通る(チャンクでも)
	small := `{"username":"u","password":"synthetic-pass-12","favorite_tags":["go"]}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(small))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", webOrigin)
	req.Header.Set("X-MTP-CSRF", "1")
	ok := httptest.NewRecorder()
	h.ServeHTTP(ok, req)
	if ok.Code != http.StatusCreated {
		t.Fatalf("small chunked body = %d: %s", ok.Code, ok.Body.String())
	}
}
