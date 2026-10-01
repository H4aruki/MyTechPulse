package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

type checker struct{ err error }

func (c checker) Ping(context.Context) error { return c.err }

func testConfig(env string, swagger bool) config.Config {
	return config.Config{Environment: env, SwaggerEnabled: swagger}
}

func newHandler(cfg config.Config, err error) http.Handler {
	h, _ := app.New(cfg, app.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Ready:  checker{err: err},
	})
	return h
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

var docPaths = []string{
	"/docs", "/openapi.json", "/openapi.yaml",
	"/openapi-3.0.json", "/openapi-3.0.yaml",
	"/schemas/ReadyOutputBody.json",
}

func TestDocsEnabledInLocalAndTest(t *testing.T) {
	for _, env := range []string{"local", "test"} {
		h := newHandler(testConfig(env, true), nil)
		for _, p := range docPaths {
			if code := get(h, p).Code; code != http.StatusOK {
				t.Errorf("%s %s = %d", env, p, code)
			}
		}
	}
}

func TestDocsDisabledInProduction(t *testing.T) {
	h := newHandler(testConfig("production", false), nil)
	for _, p := range docPaths {
		if code := get(h, p).Code; code != http.StatusNotFound {
			t.Errorf("production %s = %d", p, code)
		}
	}
	if code := get(h, "/health/live").Code; code != http.StatusOK {
		t.Errorf("live = %d", code)
	}
}

func TestReadyReturns503WhenDatabaseFails(t *testing.T) {
	h := newHandler(testConfig("local", true), errors.New("db down"))
	rec := get(h, "/health/ready")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("content type = %q", ct)
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("request id missing")
	}
	if live := get(h, "/health/live"); live.Code != http.StatusOK {
		t.Fatalf("live = %d", live.Code)
	}
}
