package recommendation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/ratelimit"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/textcheck"
)

type Handler struct {
	Service    Service
	Auth       auth.Service
	CookieName string
	// FeedLimit と ClickLimit は利用者ごとの回数制限。nilなら制限しない。
	FeedLimit  *ratelimit.Limiter
	ClickLimit *ratelimit.Limiter
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
	Tags []string `json:"tags" minItems:"1" maxItems:"50" doc:"クリックした記事タグ。1〜50件、各タグは前後の空白を除いて1〜50文字。使える文字は会員登録の favorite_tags と同じで、使えない文字を含むタグは学習に使わずに無視する(422にはしない)。サーバーのタグ一覧に無いタグは無視し、新しく登録しない。利用者ごとに1分に60回まで(まとめて20回まで)"`
}

func (h Handler) Register(api huma.API) {
	if api.OpenAPI().Components == nil {
		api.OpenAPI().Components = &huma.Components{}
	}
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	api.OpenAPI().Components.SecuritySchemes["cookieAuth"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: h.CookieName, Description: "ログインで発行されるHttpOnlyのセッションCookie"}
	huma.Register(api, huma.Operation{OperationID: "recommendation-feed", Method: http.MethodGet, Path: "/api/v1/feed", Summary: "推薦記事を取得", Description: "利用者ごとに1分に30回まで。超えると429と `Retry-After`(秒)を返す。", Tags: []string{"recommendation"}, Security: []map[string][]string{{"cookieAuth": {}}}, Middlewares: huma.Middlewares{h.readToken}, Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable}}, h.feed)
	huma.Register(api, huma.Operation{OperationID: "recommendation-clicks", Method: http.MethodPost, Path: "/api/v1/feedback/article-clicks", Summary: "記事クリックを記録", Tags: []string{"recommendation"}, Security: []map[string][]string{{"cookieAuth": {}}}, Middlewares: huma.Middlewares{h.readToken}, Description: "利用者ごとに1分に60回まで(まとめて20回まで)。超えると429と `Retry-After`(秒)を返す。", DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError}}, h.click)
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
	if err := h.FeedLimit.CheckUser(id); err != nil {
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
	if err := h.ClickLimit.CheckUser(id); err != nil {
		return nil, err
	}
	if len(in.Body.Tags) < 1 || len(in.Body.Tags) > 50 {
		return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "タグは1〜50件で指定してください")
	}
	// 長さと UTF-8 の誤りは、これまでどおり要求ごと 422 にする。
	// 使えない文字(制御文字・見えない文字・使えない記号)を含むタグは、そのタグだけ学習に使わずに捨てる。
	// タグ表に無いタグを無視するのと同じ扱いで、新しいタグが1つ混ざるだけでクリックの学習が丸ごと抜けないようにする(#261)。
	tags := make([]string, 0, len(in.Body.Tags))
	for _, raw := range in.Body.Tags {
		tag := strings.TrimSpace(raw)
		if n := utf8.RuneCountInString(tag); !utf8.ValidString(tag) || n < 1 || n > textcheck.MaxTagChars {
			return nil, httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "タグは1〜50文字で入力してください")
		}
		if textcheck.ValidTag(tag) {
			tags = append(tags, tag)
		}
	}
	if err := h.Service.RecordClick(ctx, id, tags); err != nil {
		return nil, httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
	return nil, nil
}
