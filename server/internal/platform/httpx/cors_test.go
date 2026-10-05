package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const allowedOrigin = "http://localhost:5173"

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORSPreflightFromAllowedOrigin(t *testing.T) {
	h := CORS([]string{allowedOrigin}, okHandler())
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
	req.Header.Set("Origin", allowedOrigin)
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, x-mtp-csrf")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != allowedOrigin {
		t.Fatalf("allow-origin = %q", hd.Get("Access-Control-Allow-Origin"))
	}
	if hd.Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("credentials must be allowed")
	}
	if !strings.Contains(hd.Get("Access-Control-Allow-Methods"), "POST") {
		t.Fatalf("methods = %q", hd.Get("Access-Control-Allow-Methods"))
	}
	if !strings.Contains(hd.Get("Access-Control-Allow-Headers"), CSRFHeader) {
		t.Fatalf("headers = %q", hd.Get("Access-Control-Allow-Headers"))
	}
	if !strings.Contains(strings.Join(hd.Values("Vary"), ","), "Origin") {
		t.Fatalf("Vary must include Origin: %v", hd.Values("Vary"))
	}
}

func TestCORSPreflightFromUnknownOriginIs403(t *testing.T) {
	called := false
	h := CORS([]string{allowedOrigin}, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for _, origin := range []string{"https://evil.example", "http://localhost:5173/", "http://localhost:5174", "null", "*"} {
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/auth/login", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("origin %q: status = %d", origin, rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("origin %q: CORS headers must not be set", origin)
		}
	}
	if called {
		t.Fatal("preflight must not reach the handler")
	}
}

func TestCORSActualRequestHeaders(t *testing.T) {
	h := CORS([]string{allowedOrigin}, okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Origin", allowedOrigin)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != allowedOrigin || rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("allowed origin must get credentialed CORS headers: %v", rec.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("unknown origin must not get CORS headers: %v", rec.Header())
	}

	// Originが無い通信には何も付けない
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" || len(rec.Header().Values("Vary")) != 0 {
		t.Fatalf("requests without Origin must pass through untouched: %d %v", rec.Code, rec.Header())
	}
}

func TestCORSNeverAllowsWildcardOrigin(t *testing.T) {
	h := CORS([]string{"*"}, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("wildcard must not be honored")
	}
}
