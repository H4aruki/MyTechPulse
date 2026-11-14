package httpx

import (
	"bytes"
	"errors"
	"fmt"
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
	handler := RequestID(Recover(logging.New(&logs, slog.LevelInfo), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database password must not escape")
	})))
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("X-Request-ID", "rid-panic")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(logs.String(), "password") || logs.Len() == 0 {
		t.Fatalf("unexpected log: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"request_id":"rid-panic"`) || !strings.Contains(logs.String(), `"path":"/boom"`) {
		t.Fatalf("log missing request context: %s", logs.String())
	}
}

func TestRecoverLeavesSuccessfulResponseUntouched(t *testing.T) {
	var logs bytes.Buffer
	handler := Recover(logging.New(&logs, slog.LevelInfo), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ok", nil))
	if rec.Code != http.StatusCreated || rec.Body.String() != "created" {
		t.Fatalf("応答が変わった: %d %s", rec.Code, rec.Body.String())
	}
	if logs.Len() != 0 {
		t.Fatalf("成功時にログを出さない: %s", logs.String())
	}
}

func TestRecoverRethrowsAbortHandler(t *testing.T) {
	for _, tc := range []struct {
		name  string
		panic any
	}{
		{"abort", http.ErrAbortHandler},
		{"wrapped", fmt.Errorf("client gone: %w", http.ErrAbortHandler)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			handler := Recover(logging.New(&logs, slog.LevelInfo), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic(tc.panic)
			}))
			rec := httptest.NewRecorder()
			defer func() {
				got := recover()
				err, ok := got.(error)
				if !ok || !errors.Is(err, http.ErrAbortHandler) {
					t.Fatalf("再panicではない: %v", got)
				}
				if rec.Code == http.StatusInternalServerError || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
					t.Fatalf("中断を500にしない: %d %s", rec.Code, rec.Body.String())
				}
				if logs.Len() != 0 {
					t.Fatalf("中断を障害ログにしない: %s", logs.String())
				}
			}()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/abort", nil))
			t.Fatal("http.ErrAbortHandler は呼び出し元へ戻す")
		})
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

	maxLen := strings.Repeat("a", 64)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", maxLen)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if seen != maxLen || rec.Header().Get("X-Request-ID") != maxLen {
		t.Fatalf("64文字のIDは引き継ぐ: %q", seen)
	}

	tooLong := strings.Repeat("b", 65)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", tooLong)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if seen == tooLong || len(seen) != 32 || rec.Header().Get("X-Request-ID") != seen {
		t.Fatalf("65文字のIDは作り直す: %q header %q", seen, rec.Header().Get("X-Request-ID"))
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

func TestAccessLogKeepsDefaultStatusAndAllowsFlush(t *testing.T) {
	var logs bytes.Buffer
	handler := AccessLog(logging.New(&logs, slog.LevelInfo), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, "part"); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("flush: %v", err)
		}
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/stream", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "part" || !rec.Flushed {
		t.Fatalf("stream = %d %q flushed %v", rec.Code, rec.Body.String(), rec.Flushed)
	}
	if !strings.Contains(logs.String(), `"status":200`) || strings.Contains(logs.String(), "part") {
		t.Fatalf("log = %s", logs.String())
	}
}
