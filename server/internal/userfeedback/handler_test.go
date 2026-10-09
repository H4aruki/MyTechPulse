package userfeedback_test

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
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const sessionToken = "fixed-session-token"
const origin = "http://localhost:5173"
const promptID = "11111111-1111-4111-8111-111111111111"

type sessionRepo struct{}

func (sessionRepo) FindUser(_ context.Context, hash [32]byte, _ time.Time) (auth.Session, error) {
	if hash != auth.HashToken(sessionToken) {
		return auth.Session{}, auth.ErrNotFound
	}
	return auth.Session{User: auth.User{ID: 9, Username: "fixture", Role: auth.RoleMember}}, nil
}
func (sessionRepo) Create(context.Context, [32]byte, int64, time.Time) error { return nil }
func (sessionRepo) Delete(context.Context, [32]byte) error                   { return nil }
func (sessionRepo) DeleteExpired(context.Context, time.Time) error           { return nil }

type errRepo struct {
	fakeRepo
	err error
}

func (r *errRepo) SubmitOverall(context.Context, int64, string, int64, int32, time.Time) (userfeedback.SubmitResult, error) {
	return userfeedback.SubmitResult{}, r.err
}

func newHandler(repo userfeedback.Repository) http.Handler {
	clock := &fakeClock{now: base}
	authSvc := auth.Service{Sessions: sessionRepo{}, Clock: clock}
	cfg := config.Config{Environment: "test", SwaggerEnabled: true, CORSOrigins: []string{origin}, CookieName: "mtp_session"}
	h, _ := app.New(cfg, app.Dependencies{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Auth: &authSvc, UserFeedback: &userfeedback.Service{Repo: repo, Clock: clock}})
	return h
}

func send(h http.Handler, method, path, body string, authenticated, csrf bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if csrf {
		req.Header.Set("Origin", origin)
		req.Header.Set("X-MTP-CSRF", "1")
	}
	if authenticated {
		req.AddCookie(&http.Cookie{Name: "mtp_session", Value: sessionToken})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFeedbackRoutesRequireAuthenticationAndCSRF(t *testing.T) {
	h := newHandler(&fakeRepo{form: validForm()})
	if got := send(h, http.MethodGet, "/api/v1/user-feedback/status", "", false, false); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", got.Code)
	}
	body := `{"prompt_id":"` + promptID + `"}`
	if got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", body, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("CSRFヘッダーなし = %d", got.Code)
	}
	if got := send(h, http.MethodPut, "/api/v1/user-feedback/submissions/"+promptID, `{"answers":[{"question_id":11,"score":3}]}`, true, false); got.Code != http.StatusForbidden {
		t.Fatalf("PUTもCSRF対象 = %d", got.Code)
	}
}

func TestFeedbackPresentationReturnsQuestions(t *testing.T) {
	form := validForm()
	repo := &fakeRepo{form: form, presentOut: userfeedback.Presentation{Eligible: true, Prompt: &userfeedback.PromptSummary{ID: promptID, Status: userfeedback.PromptShown, Stage: userfeedback.StageOverall, ShownAt: base}, Form: &form}}
	h := newHandler(repo)
	got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"`+promptID+`"}`, true, true)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d %s", got.Code, got.Body.String())
	}
	for _, want := range []string{`"eligible":true`, `"status":"shown"`, `"stage":"overall"`, `"prompt_id":"` + promptID + `"`, `"display_if_score_max":2`, `"key":"overall"`} {
		if !strings.Contains(got.Body.String(), want) {
			t.Fatalf("missing %s in %s", want, got.Body.String())
		}
	}
	if bad := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"x"}`, true, true); bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("UUIDでないIDは422 = %d", bad.Code)
	}
}

func TestFeedbackErrorsMapToStatusCodes(t *testing.T) {
	cases := map[error]int{userfeedback.ErrNotFound: http.StatusNotFound, userfeedback.ErrInvalidInput: http.StatusUnprocessableEntity, userfeedback.ErrConflict: http.StatusConflict, context.DeadlineExceeded: http.StatusInternalServerError}
	for err, want := range cases {
		h := newHandler(&errRepo{err: err})
		got := send(h, http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"`+promptID+`","question_id":10,"score":3}`, true, true)
		if got.Code != want || !strings.Contains(got.Header().Get("Content-Type"), "application/problem+json") {
			t.Errorf("%v: %d %s", err, got.Code, got.Header().Get("Content-Type"))
		}
	}
	h := newHandler(&fakeRepo{})
	if got := send(h, http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"`+promptID+`","question_id":10,"score":6}`, true, true); got.Code != http.StatusUnprocessableEntity {
		t.Fatalf("score 6 = %d", got.Code)
	}
}

func TestFeedbackOpenAPIContainsRoutes(t *testing.T) {
	h := newHandler(&fakeRepo{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	for _, path := range []string{"/api/v1/user-feedback/status", "/api/v1/user-feedback/presentations", "/api/v1/user-feedback/submissions", "/api/v1/user-feedback/submissions/{submission_id}", "/api/v1/user-feedback/dismissals"} {
		if !strings.Contains(rec.Body.String(), path) {
			t.Fatalf("OpenAPI missing %s", path)
		}
	}
}
