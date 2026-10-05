package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

// cookieAuthScheme はOpenAPIのセキュリティスキーム名。
const cookieAuthScheme = "cookieAuth"

// Handler は /api/v1/auth/* の経路。業務ルールは Service に任せる。
type Handler struct {
	Service Service
	// CookieName は本番では __Host-mtp_session、local/testでは mtp_session。
	CookieName string
	// CookieSecure は本番でtrue。
	CookieSecure bool
}

// SignupRequest は会員登録の入力。
type SignupRequest struct {
	Username     string   `json:"username" minLength:"1" example:"synthetic-user" doc:"利用者名。前後の空白を除いて1〜50文字"`
	Password     string   `json:"password" minLength:"1" example:"synthetic-password" doc:"パスワード。1文字以上、UTF-8で72バイト以内"`
	FavoriteTags []string `json:"favorite_tags" minItems:"1" maxItems:"128" example:"[\"go\",\"react\"]" doc:"興味のあるタグ。1〜128件、各タグは前後の空白を除いて1〜50文字。大文字小文字を無視して重複を除く"`
}

// LoginRequest はログインの入力。
type LoginRequest struct {
	Username string `json:"username" minLength:"1" example:"synthetic-user" doc:"利用者名"`
	Password string `json:"password" minLength:"1" example:"synthetic-password" doc:"パスワード"`
}

// AuthResponse は登録・ログイン成功時の本文。セッションIDは本文に含めずCookieで渡す。
type AuthResponse struct {
	User User `json:"user" doc:"ログインした利用者"`
}

type signupInput struct{ Body SignupRequest }
type loginInput struct{ Body LoginRequest }

type authOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"セッションCookie(HttpOnly、SameSite=Lax)"`
	Body      AuthResponse
}

type meOutput struct {
	Body User
}

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie" doc:"セッションCookieを破棄する指示"`
}

type tokenKey struct{}

// Register は認証APIをHumaへ登録し、OpenAPIにcookieAuthを追加する。
func (h Handler) Register(api huma.API) {
	oapi := api.OpenAPI()
	if oapi.Components == nil {
		oapi.Components = &huma.Components{}
	}
	if oapi.Components.SecuritySchemes == nil {
		oapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	oapi.Components.SecuritySchemes[cookieAuthScheme] = &huma.SecurityScheme{
		Type:        "apiKey",
		In:          "cookie",
		Name:        h.CookieName,
		Description: "ログインで発行されるHttpOnlyのセッションCookie",
	}

	const csrfNote = "ブラウザからの更新系リクエストには、許可されたOriginと `X-MTP-CSRF: 1` ヘッダーの両方が必要。"

	huma.Register(api, huma.Operation{
		OperationID:   "auth-signup",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/signup",
		Summary:       "会員登録",
		Description:   "利用者を member として登録し、選んだタグを初期の興味として保存して、そのままログイン状態にする。" + csrfNote,
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusConflict},
	}, h.signup)

	huma.Register(api, huma.Operation{
		OperationID:   "auth-login",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/login",
		Summary:       "ログイン",
		Description:   "利用者が存在しない場合と、パスワードが違う場合は、区別できない同じ401を返す。" + csrfNote,
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusOK,
		Errors:        []int{http.StatusUnauthorized},
	}, h.login)

	huma.Register(api, huma.Operation{
		OperationID: "auth-me",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/me",
		Summary:     "現在の利用者を取得",
		Tags:        []string{"auth"},
		Security:    []map[string][]string{{cookieAuthScheme: {}}},
		Middlewares: huma.Middlewares{h.readToken},
		Errors:      []int{http.StatusUnauthorized},
	}, h.me)

	huma.Register(api, huma.Operation{
		OperationID:   "auth-logout",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/logout",
		Summary:       "ログアウト",
		Description:   "現在のセッションだけを失効してCookieを破棄する。セッションが既に無くても204を返す。" + csrfNote,
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusNoContent,
		Middlewares:   huma.Middlewares{h.readToken},
		Errors:        []int{http.StatusInternalServerError},
	}, h.logout)
}

// readToken はセッションCookieの値を取り出して処理の文脈へ渡す。値はログに出さない。
func (h Handler) readToken(ctx huma.Context, next func(huma.Context)) {
	token := ""
	if c, err := huma.ReadCookie(ctx, h.CookieName); err == nil {
		token = c.Value
	}
	next(huma.WithValue(ctx, tokenKey{}, token))
}

func tokenFrom(ctx context.Context) string {
	token, _ := ctx.Value(tokenKey{}).(string)
	return token
}

func (h Handler) sessionCookie(token string, expires time.Time) http.Cookie {
	return http.Cookie{
		Name:     h.CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(h.Service.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h Handler) deleteCookie() http.Cookie {
	return http.Cookie{
		Name:     h.CookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	}
}

func (h Handler) signup(ctx context.Context, in *signupInput) (*authOutput, error) {
	user, token, expires, err := h.Service.Signup(ctx, SignupInput{
		Username:     in.Body.Username,
		Password:     in.Body.Password,
		FavoriteTags: in.Body.FavoriteTags,
	})
	if err != nil {
		return nil, toProblem(err, false)
	}
	return &authOutput{SetCookie: h.sessionCookie(token, expires), Body: AuthResponse{User: user}}, nil
}

func (h Handler) login(ctx context.Context, in *loginInput) (*authOutput, error) {
	user, token, expires, err := h.Service.Login(ctx, in.Body.Username, in.Body.Password)
	if err != nil {
		return nil, toProblem(err, false)
	}
	return &authOutput{SetCookie: h.sessionCookie(token, expires), Body: AuthResponse{User: user}}, nil
}

func (h Handler) me(ctx context.Context, _ *struct{}) (*meOutput, error) {
	session, err := h.Service.Authenticate(ctx, tokenFrom(ctx))
	if err != nil {
		return nil, toProblem(err, true)
	}
	return &meOutput{Body: session.User}, nil
}

func (h Handler) logout(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
	if err := h.Service.Logout(ctx, tokenFrom(ctx)); err != nil {
		return nil, toProblem(err, false)
	}
	return &logoutOutput{SetCookie: h.deleteCookie()}, nil
}

// toProblem は認証用途のエラーをHTTPのProblem Detailsへ変換する。
// 内部エラーの原因・SQL・入力値は応答へ出さない。
func toProblem(err error, needLogin bool) error {
	var ve *ValidationError
	switch {
	case errors.As(err, &ve):
		fields := make([]httpx.FieldError, 0, len(ve.Fields))
		for _, f := range ve.Fields {
			fields = append(fields, httpx.FieldError{Field: f.Field, Message: f.Message})
		}
		return httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "入力内容を確認してください", fields...)
	case errors.Is(err, ErrUsernameTaken):
		return httpx.NewProblem(http.StatusConflict, "username_taken", "この利用者名は既に使われています")
	case errors.Is(err, ErrInvalidCredentials):
		if needLogin {
			return httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		}
		return httpx.NewProblem(http.StatusUnauthorized, "invalid_credentials", "利用者名またはパスワードが違います")
	default:
		return httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
}
