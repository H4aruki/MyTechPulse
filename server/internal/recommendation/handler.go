package recommendation

import (
	"context"
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

type Handler struct {
	Service    Service
	Auth       auth.Service
	CookieName string
}
type tokenKey struct{}
type FeedResponse struct {
	QiitaArticles []FeedArticle `json:"qiita_articles"`
	ZennArticles  []FeedArticle `json:"zenn_articles"`
	Warnings      []Warning     `json:"warnings"`
}
type feedOutput struct{ Body FeedResponse }
type clickInput struct{ Body ClickRequest }
type ClickRequest struct {
	Tags []string `json:"tags" minItems:"1" maxItems:"50" doc:"クリックした記事タグ。1〜50件、各1〜50文字"`
}

func (h Handler) Register(api huma.API) {
	if api.OpenAPI().Components == nil {
		api.OpenAPI().Components = &huma.Components{}
	}
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	api.OpenAPI().Components.SecuritySchemes["cookieAuth"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: h.CookieName, Description: "ログインで発行されるHttpOnlyのセッションCookie"}
	huma.Register(api, huma.Operation{OperationID: "recommendation-feed", Method: http.MethodGet, Path: "/api/v1/feed", Summary: "推薦記事を取得", Tags: []string{"recommendation"}, Security: []map[string][]string{{"cookieAuth": {}}}, Middlewares: huma.Middlewares{h.readToken}, Errors: []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusServiceUnavailable}}, h.feed)
	huma.Register(api, huma.Operation{OperationID: "recommendation-clicks", Method: http.MethodPost, Path: "/api/v1/feedback/article-clicks", Summary: "記事クリックを記録", Tags: []string{"recommendation"}, Security: []map[string][]string{{"cookieAuth": {}}}, Middlewares: huma.Middlewares{h.readToken}, DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusInternalServerError}}, h.click)
}
func (h Handler) readToken(ctx huma.Context, next func(huma.Context)) {
	token := ""
	if c, err := huma.ReadCookie(ctx, h.CookieName); err == nil {
		token = c.Value
	}
	next(huma.WithValue(ctx, tokenKey{}, token))
}
func requestToken(ctx context.Context) string {
	token, _ := ctx.Value(tokenKey{}).(string)
	return token
}
func (h Handler) userID(ctx context.Context) (int64, error) {
	session, err := h.Auth.Authenticate(ctx, requestToken(ctx))
	if err != nil {
		return 0, httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
	}
	return session.User.ID, nil
}
func (h Handler) feed(ctx context.Context, _ *struct{}) (*feedOutput, error) {
	id, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	result, err := h.Service.Get(ctx, id)
	if errors.Is(err, ErrAllProvidersFailed) {
		return nil, httpx.NewProblem(http.StatusServiceUnavailable, "providers_unavailable", "記事提供元を利用できません")
	}
	if err != nil {
		return nil, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
	return &feedOutput{Body: FeedResponse{QiitaArticles: feedArticles(result.QiitaArticles), ZennArticles: feedArticles(result.ZennArticles), Warnings: result.Warnings}}, nil
}
func (h Handler) click(ctx context.Context, in *clickInput) (*struct{}, error) {
	id, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	if len(in.Body.Tags) < 1 || len(in.Body.Tags) > 50 {
		return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "タグは1〜50件で指定してください")
	}
	for _, tag := range in.Body.Tags {
		n := utf8.RuneCountInString(tag)
		if !utf8.ValidString(tag) || n < 1 || n > 50 {
			return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "タグは1〜50文字で入力してください")
		}
	}
	if err := h.Service.RecordClick(ctx, id, in.Body.Tags); err != nil {
		return nil, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
	return nil, nil
}
