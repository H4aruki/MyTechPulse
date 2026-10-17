package passkey

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

const (
	cookieAuthScheme = "cookieAuth"
	noStore          = "no-store"
)

// Handler は /api/v1/auth/passkeys/* と /api/v1/auth/passkeys/login/* の経路。
// 登録・一覧・名前変更・削除はログイン済みの利用者だけが使え、ログイン(login/begin と login/finish)は未ログインで使う。
type Handler struct {
	Service Service
	Auth    auth.Service
	// CookieName はセッションCookieの名前。
	CookieName string
	// CookieSecure は本番でtrue。セッションCookieの名前と属性が変わる。
	CookieSecure bool
	// ClientIPHeader は接続元IPを読むヘッダー名。パスワードログインと同じ値を渡す。
	ClientIPHeader string
}

// PasskeyItem は登録済みのパスキー1件。公開鍵や認証情報IDは返さない。
type PasskeyItem struct {
	ID         int64      `json:"id" doc:"パスキーのID"`
	Name       string     `json:"name" doc:"利用者が付けた名前"`
	CreatedAt  time.Time  `json:"created_at" doc:"登録日時"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty" doc:"最後に使った日時。まだ使っていなければ省略"`
}

// PasskeyListResponse はパスキーの一覧。
type PasskeyListResponse struct {
	Passkeys []PasskeyItem `json:"passkeys" doc:"登録順"`
}

// RegisterBeginResponse は端末へ渡す登録の設定。
type RegisterBeginResponse struct {
	ChallengeToken string         `json:"challengeToken" doc:"この手続きを結び付ける使い捨てのトークン。5分で失効する。完了の呼び出しでそのまま返す。保存や記録をしない"`
	PublicKey      map[string]any `json:"publicKey" doc:"navigator.credentials.create() の publicKey に渡す値(WebAuthnのJSON形式。binary項目はbase64url文字列)"`
}

// RegisterFinishRequest は登録の完了の入力。
type RegisterFinishRequest struct {
	ChallengeToken string         `json:"challengeToken" minLength:"1" maxLength:"128" doc:"開始で受け取ったトークン"`
	Name           string         `json:"name,omitempty" maxLength:"64" example:"iPhone" doc:"パスキーの名前。省略すると「パスキー」。前後の空白を除いて1〜64文字"`
	Credential     map[string]any `json:"credential" doc:"navigator.credentials.create() の結果(PublicKeyCredentialのJSON表現。binary項目はbase64url文字列)"`
}

// PasskeyLoginBeginResponse は端末へ渡すログインの設定。
type PasskeyLoginBeginResponse struct {
	ChallengeToken string         `json:"challengeToken" doc:"この手続きを結び付ける使い捨てのトークン。5分で失効する。完了の呼び出しでそのまま返す。保存や記録をしない"`
	PublicKey      map[string]any `json:"publicKey" doc:"navigator.credentials.get() の publicKey に渡す値(WebAuthnのJSON形式。binary項目はbase64url文字列)。利用者名なしで選ばせるため allowCredentials は空"`
}

// PasskeyLoginFinishRequest はログインの完了の入力。
type PasskeyLoginFinishRequest struct {
	ChallengeToken string         `json:"challengeToken" minLength:"1" maxLength:"128" doc:"開始で受け取ったトークン"`
	Credential     map[string]any `json:"credential" doc:"navigator.credentials.get() の結果(PublicKeyCredentialのJSON表現。binary項目はbase64url文字列)"`
}

// RenameRequest は名前変更の入力。
type RenameRequest struct {
	Name string `json:"name" minLength:"1" maxLength:"64" example:"仕事用PC" doc:"新しい名前。前後の空白を除いて1〜64文字"`
}

type beginOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         RegisterBeginResponse
}
type finishInput struct{ Body RegisterFinishRequest }
type finishOutput struct {
	Body PasskeyItem
}
type loginBeginOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         PasskeyLoginBeginResponse
}
type loginFinishInput struct{ Body PasskeyLoginFinishRequest }
type loginFinishOutput struct {
	CacheControl string        `header:"Cache-Control"`
	SetCookie    []http.Cookie `header:"Set-Cookie" doc:"セッションCookie(HttpOnly、SameSite=Lax)"`
	Body         auth.AuthResponse
}
type listOutput struct {
	CacheControl string `header:"Cache-Control"`
	Body         PasskeyListResponse
}
type renameInput struct {
	ID   int64 `path:"id" minimum:"1" doc:"パスキーのID"`
	Body RenameRequest
}
type deleteInput struct {
	ID int64 `path:"id" minimum:"1" doc:"パスキーのID"`
}

type tokenKey struct{}

// Register はパスキーAPIをHumaへ登録する。
func (h Handler) Register(api huma.API) {
	oapi := api.OpenAPI()
	if oapi.Components == nil {
		oapi.Components = &huma.Components{}
	}
	if oapi.Components.SecuritySchemes == nil {
		oapi.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	oapi.Components.SecuritySchemes[cookieAuthScheme] = &huma.SecurityScheme{
		Type: "apiKey", In: "cookie", Name: h.CookieName, Description: "ログインで発行されるHttpOnlyのセッションCookie",
	}

	const csrfNote = "ブラウザからの更新系リクエストには、許可されたOriginと `X-MTP-CSRF: 1` ヘッダーの両方が必要。"
	security := []map[string][]string{{cookieAuthScheme: {}}}
	mw := huma.Middlewares{h.readSession}
	tags := []string{"auth"}

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-register-begin", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/register/begin",
		Summary:     "パスキー登録の開始",
		Description: "端末へ渡す登録の設定と、手続きを結び付ける使い捨ての `challengeToken` を返す。トークンは5分で失効し、1回しか使えない。Cookieは使わないので、複数のタブで同時に手続きできる。登録済みのパスキーは `excludeCredentials` に入る。端末の本人確認(userVerification)と、利用者名なしで選べるパスキー(discoverable credential)を必須にする。上限は1人10件。" + csrfNote,
		Tags:        tags, Security: security, Middlewares: mw, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusUnauthorized, http.StatusConflict},
	}, h.registerBegin)

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-register-finish", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/register/finish",
		Summary:     "パスキー登録の完了",
		Description: "開始で受け取った `challengeToken` と端末の応答を送る。検証して保存する。トークンは検証の成否にかかわらず、この呼び出しで使い切る。他の利用者が開始したトークンは使えない。パスワードの再入力は求めない。" + csrfNote,
		Tags:        tags, Security: security, Middlewares: mw, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusConflict, http.StatusUnprocessableEntity},
	}, h.registerFinish)

	// ログインは未ログインの利用者が使うので、セキュリティ要件は付けない
	loginMW := huma.Middlewares{h.readSession, auth.ClientIPMiddleware(h.ClientIPHeader)}
	const loginNote = "接続元IPごと(15分に20回失敗で、窓が終わるまで)に失敗を数え、超えると429と `Retry-After`(秒)を返す。数えるのはパスワードのログインと同じ窓。"

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-login-begin", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/login/begin",
		Summary:     "パスキーでのログインの開始",
		Description: "利用者名なしでパスキーを選ばせる設定と、手続きを結び付ける使い捨ての `challengeToken` を返す。トークンは5分で失効し、1回しか使えない。Cookieは使わないので、複数のタブで同時にログインの手続きができる。開始は失敗として数えないが、締め出し中のIPには429を返す。開始そのものもIPごとに15分で60回まで数え(失敗とは別の枠。成功・失敗で戻さない)、超えると429と `Retry-After`(秒)を返す。" + loginNote + csrfNote,
		Tags:        tags, Middlewares: loginMW, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusTooManyRequests},
	}, h.loginBegin)

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-login-finish", Method: http.MethodPost, Path: "/api/v1/auth/passkeys/login/finish",
		Summary:     "パスキーでのログインの完了",
		Description: "開始で受け取った `challengeToken` と端末の応答を検証し、成功すればパスワードのログインと同じ属性のセッションCookieを発行する。トークンは検証の成否にかかわらず、この呼び出しで使い切る。トークンの不正・期限切れ・再利用、未知のパスキー、署名やオリジンの不一致、認証回数の巻き戻りなど、失敗の原因は区別できない同じ401を返す。" + loginNote + csrfNote,
		Tags:        tags, Middlewares: loginMW, DefaultStatus: http.StatusOK,
		Errors: []int{http.StatusUnauthorized, http.StatusTooManyRequests},
	}, h.loginFinish)

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-list", Method: http.MethodGet, Path: "/api/v1/auth/passkeys",
		Summary:     "パスキーの一覧",
		Description: "自分のパスキーだけを登録順に返す。",
		Tags:        tags, Security: security, Middlewares: mw,
		Errors: []int{http.StatusUnauthorized},
	}, h.list)

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-rename", Method: http.MethodPatch, Path: "/api/v1/auth/passkeys/{id}",
		Summary:     "パスキーの名前変更",
		Description: "自分のパスキーだけを変えられる。他人のもの・無いものは区別せず404。" + csrfNote,
		Tags:        tags, Security: security, Middlewares: mw, DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, h.rename)

	huma.Register(api, huma.Operation{
		OperationID: "auth-passkey-delete", Method: http.MethodDelete, Path: "/api/v1/auth/passkeys/{id}",
		Summary:     "パスキーの削除",
		Description: "自分のパスキーだけを物理削除する。他人のもの・無いものは区別せず404。パスワードの再入力は求めない。" + csrfNote,
		Tags:        tags, Security: security, Middlewares: mw, DefaultStatus: http.StatusNoContent,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, h.delete)
}

// readSession はセッションCookieの値を処理の文脈へ渡す。値はログに出さない。
func (h Handler) readSession(ctx huma.Context, next func(huma.Context)) {
	session := ""
	if c, err := huma.ReadCookie(ctx, h.CookieName); err == nil {
		session = c.Value
	}
	next(huma.WithValue(ctx, tokenKey{}, session))
}

func (h Handler) user(ctx context.Context) (auth.User, error) {
	token, _ := ctx.Value(tokenKey{}).(string)
	session, err := h.Auth.Authenticate(ctx, token)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			return auth.User{}, httpx.NewProblem(http.StatusUnauthorized, "unauthenticated", "ログインが必要です")
		}
		return auth.User{}, problem(err)
	}
	return session.User, nil
}

func toItem(c Credential) PasskeyItem {
	return PasskeyItem{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt, LastUsedAt: c.LastUsedAt}
}

func (h Handler) registerBegin(ctx context.Context, _ *struct{}) (*beginOutput, error) {
	user, err := h.user(ctx)
	if err != nil {
		return nil, err
	}
	token, creation, err := h.Service.BeginRegistration(ctx, user.ID, user.Username)
	if err != nil {
		return nil, problem(err)
	}
	raw, err := json.Marshal(creation)
	if err != nil {
		return nil, problem(err)
	}
	var body RegisterBeginResponse
	if err := json.Unmarshal(raw, &body); err != nil || body.PublicKey == nil {
		return nil, problem(errors.New("passkey: 登録の設定を作れません"))
	}
	body.ChallengeToken = token
	return &beginOutput{CacheControl: noStore, Body: body}, nil
}

func (h Handler) registerFinish(ctx context.Context, in *finishInput) (*finishOutput, error) {
	user, err := h.user(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(in.Body.Credential)
	if err != nil {
		return nil, problem(ErrVerificationFailed)
	}
	saved, err := h.Service.FinishRegistration(ctx, user.ID, user.Username, in.Body.ChallengeToken, in.Body.Name, raw)
	if err != nil {
		return nil, problem(err)
	}
	return &finishOutput{Body: toItem(saved)}, nil
}

func (h Handler) loginBegin(ctx context.Context, _ *struct{}) (*loginBeginOutput, error) {
	token, assertion, err := h.Service.BeginLogin(ctx, auth.ClientIPFrom(ctx))
	if err != nil {
		return nil, loginProblem(err)
	}
	raw, err := json.Marshal(assertion)
	if err != nil {
		return nil, loginProblem(err)
	}
	var body PasskeyLoginBeginResponse
	if err := json.Unmarshal(raw, &body); err != nil || body.PublicKey == nil {
		return nil, loginProblem(errors.New("passkey: ログインの設定を作れません"))
	}
	body.ChallengeToken = token
	return &loginBeginOutput{CacheControl: noStore, Body: body}, nil
}

func (h Handler) loginFinish(ctx context.Context, in *loginFinishInput) (*loginFinishOutput, error) {
	ip := auth.ClientIPFrom(ctx)
	raw, err := json.Marshal(in.Body.Credential)
	if err != nil {
		return nil, loginProblem(h.Service.rejectLogin(ip, "parse"))
	}
	userID, err := h.Service.FinishLogin(ctx, ip, in.Body.ChallengeToken, raw)
	if err != nil {
		return nil, loginProblem(err)
	}
	user, session, expires, err := h.Auth.StartSession(ctx, userID)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		// 検証の後で利用者が居なくなっていた。他の失敗と同じ応答にする
		err = h.Service.rejectLogin(ip, "user")
	}
	if err != nil {
		return nil, loginProblem(err)
	}
	return &loginFinishOutput{
		CacheControl: noStore,
		SetCookie:    []http.Cookie{auth.SessionCookie(h.CookieName, h.CookieSecure, h.Auth.SessionTTL, session, expires)},
		Body:         auth.AuthResponse{User: user},
	}, nil
}

func (h Handler) list(ctx context.Context, _ *struct{}) (*listOutput, error) {
	user, err := h.user(ctx)
	if err != nil {
		return nil, err
	}
	creds, err := h.Service.List(ctx, user.ID)
	if err != nil {
		return nil, problem(err)
	}
	items := make([]PasskeyItem, 0, len(creds))
	for _, c := range creds {
		items = append(items, toItem(c))
	}
	return &listOutput{CacheControl: noStore, Body: PasskeyListResponse{Passkeys: items}}, nil
}

func (h Handler) rename(ctx context.Context, in *renameInput) (*struct{}, error) {
	user, err := h.user(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Service.Rename(ctx, user.ID, in.ID, in.Body.Name); err != nil {
		return nil, problem(err)
	}
	return nil, nil
}

func (h Handler) delete(ctx context.Context, in *deleteInput) (*struct{}, error) {
	user, err := h.user(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Service.Delete(ctx, user.ID, in.ID); err != nil {
		return nil, problem(err)
	}
	return nil, nil
}

// loginProblem はパスキーのログインのエラーをProblem Detailsへ変換する。
// 失敗は原因によらず、本文までバイト単位で同じ401にする。締め出しは429と Retry-After。それ以外は500。
func loginProblem(err error) error {
	var tooMany *auth.TooManyAttemptsError
	switch {
	case errors.As(err, &tooMany):
		return auth.LoginProblem(err)
	case errors.Is(err, ErrLoginFailed), errors.Is(err, auth.ErrInvalidCredentials):
		return httpx.NewProblem(http.StatusUnauthorized, "invalid_credentials", "パスキーでログインできませんでした。もう一度お試しください")
	default:
		return httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
}

// problem はパスキー用途のエラーをProblem Detailsへ変換する。内部エラーの原因や入力値は応答へ出さない。
func problem(err error) error {
	var existing huma.StatusError
	if errors.As(err, &existing) {
		return err
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return httpx.NewProblem(http.StatusNotFound, "not_found", "対象のパスキーが見つかりません")
	case errors.Is(err, ErrLimitReached):
		return httpx.NewProblem(http.StatusConflict, "passkey_limit_reached", "登録できるパスキーは10件までです。不要なものを削除してからお試しください")
	case errors.Is(err, ErrCredentialExists):
		return httpx.NewProblem(http.StatusConflict, "passkey_already_registered", "このパスキーはすでに登録されています")
	case errors.Is(err, ErrChallengeInvalid):
		return httpx.NewProblem(http.StatusBadRequest, "passkey_challenge_invalid", "登録の手続きが無効か、有効期限が切れています。もう一度最初からお試しください")
	case errors.Is(err, ErrVerificationFailed):
		return httpx.NewProblem(http.StatusBadRequest, "passkey_verification_failed", "端末の応答を確認できませんでした。もう一度お試しください")
	case errors.Is(err, ErrInvalidName):
		return httpx.NewProblem(http.StatusUnprocessableEntity, "validation_failed", "入力内容を確認してください",
			httpx.FieldError{Field: "name", Message: "名前は前後の空白を除いて1〜64文字で入力してください"})
	default:
		return httpx.NewProblem(http.StatusInternalServerError, "internal_error", "サーバー内部でエラーが発生しました")
	}
}
