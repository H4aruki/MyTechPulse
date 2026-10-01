package health_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/H4aruki/MyTechPulse/server/internal/health"
)

type stubChecker struct{ err error }

func (s stubChecker) Ping(context.Context) error { return s.err }

func TestLiveAlwaysOK(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("t", "1"))
	health.Register(api, stubChecker{err: errors.New("down")})
	resp := api.Get("/health/live")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"ok"`) {
		t.Fatalf("live: %d %s", resp.Code, resp.Body.String())
	}
}

func TestReadyOKWhenDatabaseUp(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("t", "1"))
	health.Register(api, stubChecker{})
	resp := api.Get("/health/ready")
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"status":"ok"`) {
		t.Fatalf("ready: %d %s", resp.Code, resp.Body.String())
	}
}

func TestReadyProblemDoesNotLeakError(t *testing.T) {
	_, api := humatest.New(t, huma.DefaultConfig("t", "1"))
	health.Register(api, stubChecker{err: errors.New("password=secret host=db")})
	resp := api.Get("/health/ready")
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d", resp.Code)
	}
	if ct := resp.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("content type = %q", ct)
	}
	if strings.Contains(resp.Body.String(), "secret") {
		t.Fatalf("leaked: %s", resp.Body.String())
	}
}
