package savedarticle

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/ratelimit"
)

type Handler struct {
	Service    *Service
	Auth       auth.Service
	CookieName string
	// WriteLimit は保存・解除(PUT・DELETE)で共有する、利用者ごとの回数制限。nilなら制限しない。
	WriteLimit *ratelimit.Limiter
}

type tokenKey struct{}

// ArticleItem は保存した記事1件。
type ArticleItem struct {
	ID          int64     `json:"id" doc:"記事ID"`
	Source      string    `json:"source" doc:"提供元(qiita または zenn)"`
	URL         string    `json:"url" doc:"記事のURL"`
	Title       string    `json:"title" doc:"記事のタイトル"`
	AuthorName  string    `json:"author_name" doc:"著者名"`
	Likes       int       `json:"likes" doc:"いいね数"`
	PublishedAt time.Time `json:"published_at" doc:"記事の公開日時"`
	SavedAt     time.Time `json:"saved_at" doc:"保存した日時"`
}

// ListResponse は保存した記事の一覧。
type ListResponse struct {
	Items  []ArticleItem `json:"items" doc:"保存の新しい順の記事"`
	Total  int           `json:"total" doc:"保存している記事の総件数(ページングする前)"`
	Limit  int           `json:"limit" doc:"このページの最大件数"`
	Offset int           `json:"offset" doc:"このページの先頭の位置(0始まり)"`
}

type articlePath struct {
	ArticleID int64 `path:"article_id" minimum:"1" doc:"記事ID(1以上の整数)"`
}
type saveInput = articlePath
type saveOutput struct {
	Status int
}
type listInput struct {
	Limit  int `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"1ページの件数。1〜100、既定20"`
	Offset int `query:"offset" default:"0" minimum:"0" doc:"先頭から読み飛ばす件数。0以上"`
}
type listOutput struct{ Body ListResponse }

const writeLimitDoc = "利用者ごとに1分に30回まで(保存と解除を合わせて数える)。超えると429と `Retry-After`(秒)を返す。"

func (h Handler) Register(api huma.API) {
	if api.OpenAPI().Components == nil {
		api.OpenAPI().Components = &huma.Components{}
	}
	if api.OpenAPI().Components.SecuritySchemes == nil {
		api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	api.OpenAPI().Components.SecuritySchemes["cookieAuth"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: h.CookieName, Description: "ログインで発行されるHttpOnlyのセッションCookie"}
	security := []map[string][]string{{"cookieAuth": {}}}
	tags := []string{"saved-articles"}
	mw := huma.Middlewares{h.readToken}
	huma.Register(api, huma.Operation{
		OperationID: "saved-articles-save", Method: http.MethodPut, Path: "/api/v1/saved-articles/{article_id}",
		Summary:     "記事を保存",
		Description: "冪等。新しく保存したら201、すでに保存済みなら200(どちらも本文なし。保存日時は変えない)。保存は1人500件まで(保存済みの記事は上限でも200)。" + writeLimitDoc,
		Tags:        tags, Security: security, Middlewares: mw, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError},
	}, h.save)
	huma.Register(api, huma.Operation{
		OperationID: "saved-articles-unsave", Method: http.MethodDelete, Path: "/api/v1/saved-articles/{article_id}",
		Summary:     "記事の保存を解除",
		Description: "冪等。保存していない記事でも、存在しない記事IDでも204を返す(存在を探られないよう、404にしない)。" + writeLimitDoc,
		Tags:        tags, Security: security, Middlewares: mw, DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusInternalServerError},
	}, h.unsave)
	huma.Register(api, huma.Operation{
		OperationID: "saved-articles-list", Method: http.MethodGet, Path: "/api/v1/saved-articles",
		Summary:     "保存した記事の一覧",
		Description: "自分が保存した記事だけを、保存の新しい順に返す。個別の回数制限はない(`/api/v1/` 全体の接続元IP制限だけ)。",
		Tags:        tags, Security: security, Middlewares: mw,
		Errors: []int{http.StatusUnauthorized, http.StatusUnprocessableEntity, http.StatusInternalServerError},
	}, h.list)
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
		return 0, unauthenticated()
	}
	return session.User.ID, nil
}

func unauthenticated() error {
	return httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
}

func internalError() error {
	return httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
}

func (h Handler) save(ctx context.Context, in *saveInput) (*saveOutput, error) {
	id, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.WriteLimit.CheckUser(id); err != nil {
		return nil, err
	}
	created, err := h.Service.Save(ctx, id, in.ArticleID)
	switch {
	case errors.Is(err, ErrUserNotFound):
		return nil, unauthenticated() // セッションはあるが利用者の行が無い
	case errors.Is(err, ErrArticleNotFound):
		return nil, httpx.NewProblem(http.StatusNotFound, "article_not_found", "記事が見つかりません")
	case errors.Is(err, ErrLimitReached):
		return nil, httpx.NewProblem(http.StatusConflict, "saved_limit_reached", "保存は500件まで。いらない保存を外してください")
	case err != nil:
		return nil, internalError()
	}
	if created {
		return &saveOutput{Status: http.StatusCreated}, nil
	}
	return &saveOutput{Status: http.StatusOK}, nil
}

// unsave は保存を解除する。保存していなくても、記事が存在しなくても 204(冪等)。
// 記事の有無で応答を変えないので、存在しない記事IDを探る手がかりにもならない。
func (h Handler) unsave(ctx context.Context, in *articlePath) (*struct{}, error) {
	id, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.WriteLimit.CheckUser(id); err != nil {
		return nil, err
	}
	if err := h.Service.Unsave(ctx, id, in.ArticleID); err != nil {
		return nil, internalError()
	}
	return nil, nil
}

func (h Handler) list(ctx context.Context, in *listInput) (*listOutput, error) {
	id, err := h.userID(ctx)
	if err != nil {
		return nil, err
	}
	items, total, err := h.Service.List(ctx, id, in.Limit, in.Offset)
	if err != nil {
		return nil, internalError()
	}
	out := make([]ArticleItem, 0, len(items))
	for _, a := range items {
		out = append(out, ArticleItem{ID: a.ID, Source: a.Source, URL: a.URL, Title: a.Title, AuthorName: a.AuthorName, Likes: a.Likes, PublishedAt: a.PublishedAt, SavedAt: a.SavedAt})
	}
	return &listOutput{Body: ListResponse{Items: out, Total: total, Limit: in.Limit, Offset: in.Offset}}, nil
}
