package auth

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
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
	// ClientIPHeader は接続元IPを読むヘッダー名（例: CF-Connecting-IP）。空なら接続元アドレスを使う。
	// 信頼できる中継(Cloudflare等)の裏でだけ設定する。
	ClientIPHeader string
}

// SignupRequest は会員登録の入力。
type SignupRequest struct {
	Username     string   `json:"username" minLength:"1" example:"synthetic-user" doc:"利用者名。前後の空白を除いて1〜50文字。制御文字・見えない文字・改行・タブは使えない"`
	Password     string   `json:"password" minLength:"1" example:"synthetic-password" doc:"パスワード。12文字以上、UTF-8で72バイト以内。よく使われるパスワードと、利用者名と同じ文字列は使えない"`
	FavoriteTags []string `json:"favorite_tags" minItems:"1" maxItems:"128" example:"[\"go\",\"react\"]" doc:"興味のあるタグ。1〜128件、各タグは前後の空白を除いて1〜50文字。文字・数字・半角の空白(続けて並べない)と . + # - _ / ( ) （ ） & ' ・ だけが使え、見えない文字・改行・タブは使えない。大文字小文字を無視して重複を除く。サーバーのタグ一覧にあるタグだけ選べる(知らないタグを含むと422)"`
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
		Description:   "利用者を member として登録し、選んだタグを初期の興味として保存して、そのままログイン状態にする。接続元IPごとに、試行は1時間に60回(まとめては20回まで)、失敗(409・422)は15分に10回までで、超えると429と `Retry-After`(秒)を返す。失敗の枠を使い切ると、正しい入力でも枠が戻るまで429になる。" + csrfNote,
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}, h.signup)

	huma.Register(api, huma.Operation{
		OperationID:   "auth-login",
		Method:        http.MethodPost,
		Path:          "/api/v1/auth/login",
		Summary:       "ログイン",
		Description:   "利用者が存在しない場合と、パスワードが違う場合は、区別できない同じ401を返す。利用者名ごと(15分に5回失敗で、1分から最大15分まで倍々に伸びる締め出し)と接続元IPごと(15分に20回失敗で、窓が終わるまで)に失敗を数え、超えると429と `Retry-After`(秒)を返す。成功すると利用者名の数えは戻る。" + csrfNote,
		Tags:          []string{"auth"},
		DefaultStatus: http.StatusOK,
		Middlewares:   huma.Middlewares{h.readClientIP},
		Errors:        []int{http.StatusUnauthorized, http.StatusTooManyRequests},
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

// readClientIP は接続元IPを取り出して処理の文脈へ渡す。
func (h Handler) readClientIP(ctx huma.Context, next func(huma.Context)) {
	ClientIPMiddleware(h.ClientIPHeader)(ctx, next)
}

// ClientIPMiddleware は接続元IPを処理の文脈へ載せるミドルウェアを返す。
// header が空でなければ、そのヘッダーの値を優先する(信頼できる中継の裏でだけ設定する)。
// パスキーのログインも、パスワードログインと同じ規則でIPを決めるために使う。
func ClientIPMiddleware(header string) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		value := ""
		if header != "" {
			value = ctx.Header(header)
		}
		next(huma.WithValue(ctx, clientIPKey{}, clientIP(value, ctx.RemoteAddr())))
	}
}

// ClientIPOfRequest は net/http の要求から、ClientIPMiddleware と同じ規則で接続元IPを決める。
// Huma を通らない共通の制限(app の回数制限)が、ログインの制限と同じIPを使うために使う。
func ClientIPOfRequest(header string, r *http.Request) string {
	value := ""
	if header != "" {
		value = r.Header.Get(header)
	}
	return clientIP(value, r.RemoteAddr)
}

// clientIP は信頼するヘッダーに値があればそれを、無ければ接続元アドレスのIPを返す。
func clientIP(headerValue, remoteAddr string) string {
	if v := strings.TrimSpace(headerValue); v != "" {
		return v
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

func tokenFrom(ctx context.Context) string {
	token, _ := ctx.Value(tokenKey{}).(string)
	return token
}

func (h Handler) sessionCookie(token string, expires time.Time) http.Cookie {
	return SessionCookie(h.CookieName, h.CookieSecure, h.Service.SessionTTL, token, expires)
}

// SessionCookie はセッションCookieを作る(HttpOnly、SameSite=Lax、Path=/)。
// パスワードとパスキーのログインで、同じ属性のCookieを発行するために共有する。
func SessionCookie(name string, secure bool, ttl time.Duration, token string, expires time.Time) http.Cookie {
	return http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   secure,
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

// LoginProblem はログイン用途のエラーをProblem Detailsへ変換する。
// 締め出しは429と Retry-After(秒)、認証の失敗は401 invalid_credentials にする。
func LoginProblem(err error) error { return toProblem(err, false) }

// toProblem は認証用途のエラーをHTTPのProblem Detailsへ変換する。
// 内部エラーの原因・SQL・入力値は応答へ出さない。
func toProblem(err error, needLogin bool) error {
	var ve *ValidationError
	var tm *TooManyAttemptsError
	switch {
	case errors.As(err, &tm):
		secs := int(math.Ceil(tm.RetryAfter.Seconds()))
		if secs < 1 {
			secs = 1
		}
		return huma.ErrorWithHeaders(
			httpx.NewProblem(http.StatusTooManyRequests, "too_many_requests", "ログインの試行が多すぎます。しばらく待ってからもう一度お試しください"),
			http.Header{"Retry-After": {strconv.Itoa(secs)}})
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
