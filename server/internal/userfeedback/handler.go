package userfeedback

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
	"github.com/danielgtaylor/huma/v2"
)

type Handler struct {
	Service    *Service
	Auth       auth.Service
	CookieName string
}
type tokenKey struct{}

type FeedbackQuestion struct {
	ID                  int64  `json:"id"`
	Key                 string `json:"key"`
	Text                string `json:"text"`
	SortOrder           int32  `json:"sort_order"`
	Required            bool   `json:"required"`
	DisplayIfQuestionID *int64 `json:"display_if_question_id,omitempty" doc:"この質問を出す条件になる質問。総合評価では省略"`
	DisplayIfScoreMax   *int32 `json:"display_if_score_max,omitempty" doc:"条件の質問の評価がこの値以下のときに出す"`
}
type FeedbackForm struct {
	Title     string             `json:"title"`
	Version   int32              `json:"version"`
	Questions []FeedbackQuestion `json:"questions"`
}
type FeedbackLastPrompt struct {
	Status           string    `json:"status" enum:"shown,dismissed,submitted"`
	Stage            string    `json:"stage" enum:"overall,followup"`
	ShownAt          time.Time `json:"shown_at"`
	SubmissionStatus string    `json:"submission_status,omitempty" enum:"partial,completed"`
}
type FeedbackStatusResponse struct {
	Eligible       bool                `json:"eligible"`
	NextEligibleAt *time.Time          `json:"next_eligible_at,omitempty"`
	LastPrompt     *FeedbackLastPrompt `json:"last_prompt,omitempty"`
}
type FeedbackPresentationRequest struct {
	PromptID string `json:"prompt_id" format:"uuid" doc:"画面が表示要求ごとに作るID。同じ要求の再送では同じ値を使う"`
}
type FeedbackPresentationResponse struct {
	Eligible       bool          `json:"eligible"`
	NextEligibleAt *time.Time    `json:"next_eligible_at,omitempty"`
	PromptID       string        `json:"prompt_id,omitempty"`
	Status         string        `json:"status,omitempty" enum:"shown,dismissed,submitted"`
	Stage          string        `json:"stage,omitempty" enum:"overall,followup"`
	SubmissionID   string        `json:"submission_id,omitempty"`
	Form           *FeedbackForm `json:"form,omitempty" doc:"status が shown のときだけ返る"`
}
type FeedbackSubmissionRequest struct {
	PromptID   string `json:"prompt_id" format:"uuid"`
	QuestionID int64  `json:"question_id" minimum:"1"`
	Score      int32  `json:"score" minimum:"1" maximum:"5"`
}
type FeedbackAnswer struct {
	QuestionID int64 `json:"question_id" minimum:"1"`
	Score      int32 `json:"score" minimum:"1" maximum:"5"`
}
type FeedbackFollowupRequest struct {
	Answers []FeedbackAnswer `json:"answers" minItems:"1" maxItems:"20"`
}
type FeedbackSubmissionResponse struct {
	SubmissionID     string `json:"submission_id"`
	Status           string `json:"status" enum:"partial,completed"`
	FollowupRequired bool   `json:"followup_required"`
}
type FeedbackDismissalRequest struct {
	PromptID string `json:"prompt_id" format:"uuid"`
}
type FeedbackDismissalResponse struct {
	PromptID string `json:"prompt_id"`
	Status   string `json:"status" enum:"shown,dismissed,submitted"`
	Stage    string `json:"stage" enum:"overall,followup"`
}

type statusOutput struct{ Body FeedbackStatusResponse }
type presentationInput struct{ Body FeedbackPresentationRequest }
type presentationOutput struct{ Body FeedbackPresentationResponse }
type submissionInput struct{ Body FeedbackSubmissionRequest }
type followupInput struct {
	SubmissionID string `path:"submission_id" format:"uuid"`
	Body         FeedbackFollowupRequest
}
type submissionOutput struct{ Body FeedbackSubmissionResponse }
type dismissalInput struct{ Body FeedbackDismissalRequest }
type dismissalOutput struct{ Body FeedbackDismissalResponse }

func (h Handler) Register(api huma.API) {
	if api.OpenAPI().Components == nil {
		api.OpenAPI().Components = &huma.Components{}
	}
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	api.OpenAPI().Components.SecuritySchemes["cookieAuth"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: h.CookieName, Description: "ログインで発行されるHttpOnlyのセッションCookie"}
	security := []map[string][]string{{"cookieAuth": {}}}
	mw := huma.Middlewares{h.readToken}
	tags := []string{"user-feedback"}
	errs := []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError}
	huma.Register(api, huma.Operation{OperationID: "user-feedback-status", Method: http.MethodGet, Path: "/api/v1/user-feedback/status", Summary: "アンケートの表示可否と直近の状態", Tags: tags, Security: security, Middlewares: mw, Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError}}, h.status)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-present", Method: http.MethodPost, Path: "/api/v1/user-feedback/presentations", Summary: "アンケートの表示を要求", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.present)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-submit", Method: http.MethodPost, Path: "/api/v1/user-feedback/submissions", Summary: "総合評価を保存", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.submit)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-complete", Method: http.MethodPut, Path: "/api/v1/user-feedback/submissions/{submission_id}", Summary: "追加質問の回答を保存", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.complete)
	huma.Register(api, huma.Operation{OperationID: "user-feedback-dismiss", Method: http.MethodPost, Path: "/api/v1/user-feedback/dismissals", Summary: "回答せず閉じたことを保存", Tags: tags, Security: security, Middlewares: mw, Errors: errs}, h.dismiss)
}

func (h Handler) readToken(ctx huma.Context, next func(huma.Context)) {
	token := ""
	if c, err := huma.ReadCookie(ctx, h.CookieName); err == nil {
		token = c.Value
	}
	next(huma.WithValue(ctx, tokenKey{}, token))
}
func (h Handler) userID(ctx context.Context) (int64, error) {
	token, _ := ctx.Value(tokenKey{}).(string)
	session, err := h.Auth.Authenticate(ctx, token)
	if err != nil {
		return 0, httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
	}
	return session.User.ID, nil
}
func problem(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NewProblem(http.StatusNotFound, "not_found", "対象のアンケートが見つかりません")
	case errors.Is(err, ErrInvalidInput):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "回答の内容が正しくありません")
	case errors.Is(err, ErrConflict):
		return httpx.NewProblem(http.StatusConflict, "conflict", "このアンケートはすでに終了しています")
	default:
		return httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
}
func toForm(f *Form) *FeedbackForm {
	if f == nil {
		return nil
	}
	out := &FeedbackForm{Title: f.Title, Version: f.Version, Questions: make([]FeedbackQuestion, 0, len(f.Questions))}
	for _, q := range f.Questions {
		out.Questions = append(out.Questions, FeedbackQuestion{ID: q.ID, Key: q.Key, Text: q.Text, SortOrder: q.SortOrder, Required: q.Required, DisplayIfQuestionID: q.DisplayIfQuestionID, DisplayIfScoreMax: q.DisplayIfScoreMax})
	}
	return out
}

func (h Handler) status(ctx context.Context, _ *struct{}) (*statusOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.Service.Status(ctx, uid)
	if err != nil {
		return nil, problem(err)
	}
	out := &statusOutput{Body: FeedbackStatusResponse{Eligible: st.Eligible, NextEligibleAt: st.NextEligibleAt}}
	if p := st.LastPrompt; p != nil {
		last := &FeedbackLastPrompt{Status: string(p.Status), Stage: string(p.Stage), ShownAt: p.ShownAt}
		if p.SubmissionStatus != nil {
			last.SubmissionStatus = string(*p.SubmissionStatus)
		}
		out.Body.LastPrompt = last
	}
	return out, nil
}
func (h Handler) present(ctx context.Context, in *presentationInput) (*presentationOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.Service.Present(ctx, uid, in.Body.PromptID)
	if err != nil {
		return nil, problem(err)
	}
	body := FeedbackPresentationResponse{Eligible: p.Eligible, NextEligibleAt: p.NextEligibleAt, Form: toForm(p.Form)}
	if p.Prompt != nil {
		body.PromptID, body.Status, body.Stage = p.Prompt.ID, string(p.Prompt.Status), string(p.Prompt.Stage)
		if p.Prompt.SubmissionID != nil && p.Prompt.SubmissionStatus != nil && *p.Prompt.SubmissionStatus == SubmissionPartial {
			body.SubmissionID = *p.Prompt.SubmissionID
		}
	}
	return &presentationOutput{Body: body}, nil
}
func (h Handler) submit(ctx context.Context, in *submissionInput) (*submissionOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.Service.SubmitOverall(ctx, uid, in.Body.PromptID, in.Body.QuestionID, in.Body.Score)
	if err != nil {
		return nil, problem(err)
	}
	return &submissionOutput{Body: FeedbackSubmissionResponse{SubmissionID: r.SubmissionID, Status: string(r.Status), FollowupRequired: r.FollowupRequired}}, nil
}
func (h Handler) complete(ctx context.Context, in *followupInput) (*submissionOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	answers := make([]Answer, 0, len(in.Body.Answers))
	for _, a := range in.Body.Answers {
		answers = append(answers, Answer{QuestionID: a.QuestionID, Score: a.Score})
	}
	r, err := h.Service.CompleteFollowup(ctx, uid, in.SubmissionID, answers)
	if err != nil {
		return nil, problem(err)
	}
	return &submissionOutput{Body: FeedbackSubmissionResponse{SubmissionID: r.SubmissionID, Status: string(r.Status), FollowupRequired: r.FollowupRequired}}, nil
}
func (h Handler) dismiss(ctx context.Context, in *dismissalInput) (*dismissalOutput, error) {
	uid, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	s, err := h.Service.Dismiss(ctx, uid, in.Body.PromptID)
	if err != nil {
		return nil, problem(err)
	}
	return &dismissalOutput{Body: FeedbackDismissalResponse{PromptID: s.ID, Status: string(s.Status), Stage: string(s.Stage)}}, nil
}
