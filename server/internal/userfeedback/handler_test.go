package userfeedback_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const sessionToken = "fixed-session-token"
const origin = "http://localhost:5173"
const promptID = "11111111-1111-4111-8111-111111111111"
const submissionID = "22222222-2222-4222-8222-222222222222"

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
	repo := &fakeRepo{form: validForm()}
	h := newHandler(repo)
	if got := send(h, http.MethodGet, "/api/v1/user-feedback/status", "", false, false); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d", got.Code)
	}
	body := `{"prompt_id":"` + promptID + `"}`
	submitBody := `{"prompt_id":"` + promptID + `","question_id":10,"score":3}`
	followupBody := `{"answers":[{"question_id":11,"score":3}]}`
	writes := []struct{ method, path, req string }{
		{http.MethodPost, "/api/v1/user-feedback/presentations", body},
		{http.MethodPost, "/api/v1/user-feedback/submissions", submitBody},
		{http.MethodPut, "/api/v1/user-feedback/submissions/" + submissionID, followupBody},
		{http.MethodPost, "/api/v1/user-feedback/dismissals", body},
	}
	for _, w := range writes {
		if got := send(h, w.method, w.path, w.req, false, true); got.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s unauthenticated = %d", w.method, w.path, got.Code)
		}
		if got := send(h, w.method, w.path, w.req, true, false); got.Code != http.StatusForbidden {
			t.Fatalf("%s %s CSRFなし = %d", w.method, w.path, got.Code)
		}
	}
	if repo.submitCalls+repo.completeCalls+repo.dismissCalls != 0 {
		t.Fatalf("未ログインやCSRFなしが保管先まで届いた submit=%d complete=%d dismiss=%d", repo.submitCalls, repo.completeCalls, repo.dismissCalls)
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

func TestFeedbackStatusReflectsHistoryAndStoreErrors(t *testing.T) {
	h := newHandler(&fakeRepo{form: validForm()})
	got := send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"eligible":true`) || strings.Contains(got.Body.String(), "last_prompt") || strings.Contains(got.Body.String(), "next_eligible_at") {
		t.Fatalf("履歴なし = %d %s", got.Code, got.Body.String())
	}

	partial := userfeedback.SubmissionPartial
	h = newHandler(&fakeRepo{form: validForm(), last: &userfeedback.PromptSummary{
		ID: "p1", Status: userfeedback.PromptSubmitted, Stage: userfeedback.StageFollowup, ShownAt: base, SubmissionStatus: &partial,
	}})
	got = send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false)
	for _, want := range []string{`"eligible":false`, `"next_eligible_at":"2026-11-30T12:00:00Z"`, `"status":"submitted"`, `"stage":"followup"`, `"shown_at":"2026-10-01T12:00:00Z"`, `"submission_status":"partial"`} {
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), want) {
			t.Fatalf("missing %s in %d %s", want, got.Code, got.Body.String())
		}
	}

	h = newHandler(&fakeRepo{form: validForm(), last: &userfeedback.PromptSummary{Status: userfeedback.PromptDismissed, Stage: userfeedback.StageOverall, ShownAt: base}})
	got = send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false)
	if got.Code != http.StatusOK || strings.Contains(got.Body.String(), "submission_status") {
		t.Fatalf("回答前の履歴に回答状態が付いた: %s", got.Body.String())
	}

	for _, formErr := range []error{userfeedback.ErrNotFound, userfeedback.ErrInvalidDefinition} {
		h = newHandler(&fakeRepo{formErr: formErr})
		got = send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false)
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"eligible":false`) {
			t.Fatalf("%v = %d %s", formErr, got.Code, got.Body.String())
		}
	}

	h = newHandler(&fakeRepo{formErr: errors.New("db down")})
	got = send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false)
	assertFeedbackInternal(t, got, "db down")

	h = newHandler(&fakeRepo{form: validForm(), lastErr: userfeedback.ErrNotFound})
	got = send(h, http.MethodGet, "/api/v1/user-feedback/status", "", true, false)
	if got.Code != http.StatusNotFound || !strings.Contains(got.Body.String(), `"code":"not_found"`) || !strings.Contains(got.Body.String(), "対象のアンケートが見つかりません") {
		t.Fatalf("last prompt = %d %s", got.Code, got.Body.String())
	}
}

func TestFeedbackPresentationEdges(t *testing.T) {
	next := base.AddDate(0, 0, 60)
	h := newHandler(&fakeRepo{presentOut: userfeedback.Presentation{Eligible: false, NextEligibleAt: &next}})
	got := send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"`+promptID+`"}`, true, true)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"eligible":false`) || !strings.Contains(got.Body.String(), `"next_eligible_at":"2026-11-30T12:00:00Z"`) || strings.Contains(got.Body.String(), `"form"`) {
		t.Fatalf("表示できないとき = %d %s", got.Code, got.Body.String())
	}

	resumeID := "33333333-3333-4333-8333-333333333333"
	partial := userfeedback.SubmissionPartial
	form := validForm()
	h = newHandler(&fakeRepo{presentOut: userfeedback.Presentation{Eligible: true, Prompt: &userfeedback.PromptSummary{
		ID: promptID, Status: userfeedback.PromptShown, Stage: userfeedback.StageFollowup, ShownAt: base, SubmissionID: &resumeID, SubmissionStatus: &partial,
	}, Form: &form}})
	got = send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"`+promptID+`"}`, true, true)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"submission_id":"`+resumeID+`"`) || !strings.Contains(got.Body.String(), `"stage":"followup"`) {
		t.Fatalf("途中の追加質問 = %d %s", got.Code, got.Body.String())
	}

	done := userfeedback.SubmissionCompleted
	h = newHandler(&fakeRepo{presentOut: userfeedback.Presentation{Eligible: true, Prompt: &userfeedback.PromptSummary{
		ID: promptID, Status: userfeedback.PromptSubmitted, Stage: userfeedback.StageOverall, SubmissionID: &resumeID, SubmissionStatus: &done,
	}, Form: &form}})
	got = send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"`+promptID+`"}`, true, true)
	if got.Code != http.StatusOK || strings.Contains(got.Body.String(), resumeID) {
		t.Fatalf("完了済みは回答IDを返さない: %d %s", got.Code, got.Body.String())
	}

	h = newHandler(&fakeRepo{presentErr: errors.New("db down")})
	assertFeedbackInternal(t, send(h, http.MethodPost, "/api/v1/user-feedback/presentations", `{"prompt_id":"`+promptID+`"}`, true, true), "db down")
}

func TestFeedbackSubmitCompleteAndDismiss(t *testing.T) {
	repo := &fakeRepo{submitResult: userfeedback.SubmitResult{SubmissionID: submissionID, Status: userfeedback.SubmissionPartial, FollowupRequired: true}}
	h := newHandler(repo)
	got := send(h, http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"`+promptID+`","question_id":10,"score":1}`, true, true)
	for _, want := range []string{`"submission_id":"` + submissionID + `"`, `"status":"partial"`, `"followup_required":true`} {
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), want) {
			t.Fatalf("missing %s in %d %s", want, got.Code, got.Body.String())
		}
	}
	if got := send(h, http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"`+promptID+`","question_id":10,"score":5}`, true, true); got.Code != http.StatusOK {
		t.Fatalf("score 5 = %d %s", got.Code, got.Body.String())
	}
	if repo.submitCalls != 2 || repo.submitUser != 9 || repo.submitPrompt != promptID || repo.submitQuestion != 10 || repo.submitScore != 5 || !repo.submitAt.Equal(base) {
		t.Fatalf("submit repo = %+v", repo)
	}

	repo = &fakeRepo{completeResult: userfeedback.SubmitResult{SubmissionID: submissionID, Status: userfeedback.SubmissionCompleted}}
	h = newHandler(repo)
	got = send(h, http.MethodPut, "/api/v1/user-feedback/submissions/"+submissionID, `{"answers":[{"question_id":1,"score":1},{"question_id":2,"score":5}]}`, true, true)
	for _, want := range []string{`"submission_id":"` + submissionID + `"`, `"status":"completed"`, `"followup_required":false`} {
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), want) {
			t.Fatalf("missing %s in %d %s", want, got.Code, got.Body.String())
		}
	}
	if repo.completeCalls != 1 || repo.completeUser != 9 || repo.completeID != submissionID || len(repo.completeAnswers) != 2 || repo.completeAnswers[0].Score != 1 || repo.completeAnswers[1].Score != 5 || !repo.completeAt.Equal(base) {
		t.Fatalf("complete repo user=%d id=%s answers=%+v at=%v", repo.completeUser, repo.completeID, repo.completeAnswers, repo.completeAt)
	}

	repo = &fakeRepo{dismissOut: userfeedback.PromptSummary{ID: promptID, Status: userfeedback.PromptDismissed, Stage: userfeedback.StageFollowup}}
	h = newHandler(repo)
	got = send(h, http.MethodPost, "/api/v1/user-feedback/dismissals", `{"prompt_id":"`+promptID+`"}`, true, true)
	for _, want := range []string{`"prompt_id":"` + promptID + `"`, `"status":"dismissed"`, `"stage":"followup"`} {
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), want) {
			t.Fatalf("missing %s in %d %s", want, got.Code, got.Body.String())
		}
	}
	if repo.dismissCalls != 1 || repo.dismissUser != 9 || repo.dismissPrompt != promptID || !repo.dismissAt.Equal(base) {
		t.Fatalf("dismiss repo user=%d prompt=%s at=%v calls=%d", repo.dismissUser, repo.dismissPrompt, repo.dismissAt, repo.dismissCalls)
	}

	for _, tc := range []struct {
		name string
		err  error
		want int
		code string
	}{
		{"not found", userfeedback.ErrNotFound, http.StatusNotFound, "not_found"},
		{"invalid", userfeedback.ErrInvalidInput, http.StatusUnprocessableEntity, "validation_failed"},
		{"conflict", errors.Join(errors.New("finished"), userfeedback.ErrConflict), http.StatusConflict, "conflict"},
		{"store", errors.New("db down"), http.StatusInternalServerError, "internal_error"},
	} {
		h = newHandler(&fakeRepo{completeErr: tc.err})
		got = send(h, http.MethodPut, "/api/v1/user-feedback/submissions/"+submissionID, `{"answers":[{"question_id":11,"score":3}]}`, true, true)
		if got.Code != tc.want || !strings.Contains(got.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("complete %s = %d %s", tc.name, got.Code, got.Body.String())
		}
		if tc.name == "store" && strings.Contains(got.Body.String(), "db down") {
			t.Errorf("complete store error leaked: %s", got.Body.String())
		}
		if tc.name == "conflict" && (strings.Contains(got.Body.String(), "finished") || !strings.Contains(got.Body.String(), "このアンケートはすでに終了しています")) {
			t.Errorf("complete conflict = %s", got.Body.String())
		}
		h = newHandler(&fakeRepo{dismissErr: tc.err})
		got = send(h, http.MethodPost, "/api/v1/user-feedback/dismissals", `{"prompt_id":"`+promptID+`"}`, true, true)
		if got.Code != tc.want || !strings.Contains(got.Body.String(), `"code":"`+tc.code+`"`) {
			t.Errorf("dismiss %s = %d %s", tc.name, got.Code, got.Body.String())
		}
		if tc.name == "store" && strings.Contains(got.Body.String(), "db down") {
			t.Errorf("dismiss store error leaked: %s", got.Body.String())
		}
	}
}

func TestFeedbackValidationRejectsBadInput(t *testing.T) {
	repo := &fakeRepo{}
	h := newHandler(repo)
	many := `{"answers":[` + strings.TrimRight(strings.Repeat(`{"question_id":1,"score":1},`, 21), ",") + `]}`
	cases := []struct{ name, method, path, body string }{
		{"submit score 0", http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"` + promptID + `","question_id":10,"score":0}`},
		{"submit score 6", http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"` + promptID + `","question_id":10,"score":6}`},
		{"submit question 0", http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"` + promptID + `","question_id":0,"score":3}`},
		{"submit bad uuid", http.MethodPost, "/api/v1/user-feedback/submissions", `{"prompt_id":"x","question_id":10,"score":3}`},
		{"complete bad uuid", http.MethodPut, "/api/v1/user-feedback/submissions/not-a-uuid", `{"answers":[{"question_id":1,"score":1}]}`},
		{"complete empty", http.MethodPut, "/api/v1/user-feedback/submissions/" + submissionID, `{"answers":[]}`},
		{"complete 21 answers", http.MethodPut, "/api/v1/user-feedback/submissions/" + submissionID, many},
		{"complete score 0", http.MethodPut, "/api/v1/user-feedback/submissions/" + submissionID, `{"answers":[{"question_id":1,"score":0}]}`},
		{"complete score 6", http.MethodPut, "/api/v1/user-feedback/submissions/" + submissionID, `{"answers":[{"question_id":1,"score":6}]}`},
		{"complete question 0", http.MethodPut, "/api/v1/user-feedback/submissions/" + submissionID, `{"answers":[{"question_id":0,"score":3}]}`},
		{"dismiss bad uuid", http.MethodPost, "/api/v1/user-feedback/dismissals", `{"prompt_id":"x"}`},
		{"dismiss missing id", http.MethodPost, "/api/v1/user-feedback/dismissals", `{}`},
	}
	for _, tc := range cases {
		got := send(h, tc.method, tc.path, tc.body, true, true)
		if got.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s = %d %s", tc.name, got.Code, got.Body.String())
		}
	}
	if repo.submitCalls+repo.completeCalls+repo.dismissCalls != 0 {
		t.Fatalf("入力の検証で保管先まで届いた submit=%d complete=%d dismiss=%d", repo.submitCalls, repo.completeCalls, repo.dismissCalls)
	}
}

func assertFeedbackInternal(t *testing.T, got *httptest.ResponseRecorder, leaked string) {
	t.Helper()
	if got.Code != http.StatusInternalServerError || !strings.Contains(got.Body.String(), `"code":"internal_error"`) || strings.Contains(got.Body.String(), leaked) {
		t.Fatalf("status = %d %s", got.Code, got.Body.String())
	}
	if !strings.Contains(got.Header().Get("Content-Type"), "application/problem+json") || !strings.Contains(got.Body.String(), "サーバー内部でエラーが発生しました") {
		t.Fatalf("problem = %s %s", got.Header().Get("Content-Type"), got.Body.String())
	}
}

// 認証方式の欄が空でも、登録したCookie名で cookieAuth を足す。
func TestRegisterAddsCookieAuthWhenSecuritySchemesAreMissing(t *testing.T) {
	cfg := huma.DefaultConfig("MyTechPulse API", "1.0.0")
	api := humago.New(http.NewServeMux(), cfg)
	api.OpenAPI().Components.SecuritySchemes = nil
	userfeedback.Handler{Service: &userfeedback.Service{Repo: &fakeRepo{}}, CookieName: "__Host-mtp_session"}.Register(api)
	scheme := api.OpenAPI().Components.SecuritySchemes["cookieAuth"]
	if scheme == nil || scheme.Type != "apiKey" || scheme.In != "cookie" || scheme.Name != "__Host-mtp_session" {
		t.Fatalf("cookieAuth = %+v", scheme)
	}
	if api.OpenAPI().Paths["/api/v1/user-feedback/status"] == nil {
		t.Fatal("feedback status path was not registered")
	}
}
