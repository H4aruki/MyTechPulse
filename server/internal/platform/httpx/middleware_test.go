package httpx

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/logging"
)

func TestAccessLogDoesNotLogHeadersOrBody(t *testing.T) {
	var logs bytes.Buffer
	logger := logging.New(&logs, slog.LevelInfo)
	handler := AccessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
	req.Header.Set("Cookie", "__Host-mtp_session=secret-session")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(logs.String(), "secret") {
		t.Fatalf("secret leaked: %s", logs.String())
	}
	for _, want := range []string{"POST", "/api/v1/auth/login", "204"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log missing %q: %s", want, logs.String())
		}
	}
}

func TestRecoverReturnsGenericProblem(t *testing.T) {
	handler := Recover(slog.New(slog.NewJSONHandler(io.Discard, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password must not escape")
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("unsafe response: %d %s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type = %q", ct)
	}
}

func TestRecoverDoesNotLogPanicValue(t *testing.T) {
	var logs bytes.Buffer
	handler := Recover(logging.New(&logs, slog.LevelInfo), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password must not escape")
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	if strings.Contains(logs.String(), "password") || logs.Len() == 0 {
		t.Fatalf("unexpected log: %s", logs.String())
	}
}

func TestRequestIDGeneratesAndPropagates(t *testing.T) {
	var seen string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if len(seen) != 32 || rec.Header().Get("X-Request-ID") != seen {
		t.Fatalf("generated id = %q, header = %q", seen, rec.Header().Get("X-Request-ID"))
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "abc-123_OK")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if seen != "abc-123_OK" || rec.Header().Get("X-Request-ID") != "abc-123_OK" {
		t.Fatalf("valid id not kept: %q", seen)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "bad id\twith spaces")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if seen == "bad id\twith spaces" || len(seen) != 32 {
		t.Fatalf("invalid id kept: %q", seen)
	}
}

func TestAccessLogIncludesRequestID(t *testing.T) {
	var logs bytes.Buffer
	logger := logging.New(&logs, slog.LevelInfo)
	handler := RequestID(AccessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-ID", "rid-1")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(logs.String(), `"request_id":"rid-1"`) {
		t.Fatalf("request_id missing: %s", logs.String())
	}
}
