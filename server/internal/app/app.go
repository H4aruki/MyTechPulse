// Package app はHumaと各経路・共通処理を組み立てる。
package app

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/health"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

// Dependencies は app が外から受け取る依存。
type Dependencies struct {
	Logger *slog.Logger
	Ready  health.ReadyChecker
	// Auth が nil のときは認証APIを登録しない。
	Auth           *auth.Service
	Recommendation *recommendation.Service
	UserFeedback   *userfeedback.Service
}

// New はHTTPハンドラーとOpenAPI仕様を返す。
func New(cfg config.Config, deps Dependencies) (http.Handler, *huma.OpenAPI) {
	mux := http.NewServeMux()
	hc := huma.DefaultConfig("MyTechPulse API", "1.0.0")
	hc.DocsRenderer = huma.DocsRendererSwaggerUI
	hc.CreateHooks = nil // 応答本文へ $schema を足さない(契約は {"user": ...} のまま)
	hc.DocsPath = "/docs"
	hc.OpenAPIPath = "/openapi" // Humaが.jsonと.yamlを付けて公開する基底パス
	if !cfg.SwaggerEnabled {
		hc.DocsPath, hc.OpenAPIPath, hc.SchemasPath = "", "", ""
	}
	httpx.UseProblemErrors()
	api := humago.New(mux, hc)
	health.Register(api, deps.Ready)
	if deps.Auth != nil {
		auth.Handler{Service: *deps.Auth, CookieName: cfg.CookieName, CookieSecure: cfg.CookieSecure}.Register(api)
	}
	if deps.Auth != nil && deps.Recommendation != nil {
		recommendation.Handler{Service: *deps.Recommendation, Auth: *deps.Auth, CookieName: cfg.CookieName}.Register(api)
	}
	if deps.Auth != nil && deps.UserFeedback != nil {
		userfeedback.Handler{Service: deps.UserFeedback, Auth: *deps.Auth, CookieName: cfg.CookieName}.Register(api)
	}
	// 外側から RequestID -> Recover -> AccessLog -> CORS -> CSRF -> Huma の順に通す
	var h http.Handler = mux
	h = httpx.CSRF(cfg.CORSOrigins, h)
	h = httpx.CORS(cfg.CORSOrigins, h)
	h = httpx.AccessLog(deps.Logger, h)
	h = httpx.Recover(deps.Logger, h)
	h = httpx.RequestID(h)
	return h, api.OpenAPI()
}
