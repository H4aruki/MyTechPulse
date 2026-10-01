// Package app はHumaと各経路・共通処理を組み立てる。
package app

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/H4aruki/MyTechPulse/server/internal/health"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/httpx"
)

// Dependencies は app が外から受け取る依存。
type Dependencies struct {
	Logger *slog.Logger
	Ready  health.ReadyChecker
}

// New はHTTPハンドラーとOpenAPI仕様を返す。
func New(cfg config.Config, deps Dependencies) (http.Handler, *huma.OpenAPI) {
	mux := http.NewServeMux()
	hc := huma.DefaultConfig("MyTechPulse API", "1.0.0")
	hc.DocsRenderer = huma.DocsRendererSwaggerUI
	hc.DocsPath = "/docs"
	hc.OpenAPIPath = "/openapi" // Humaが.jsonと.yamlを付けて公開する基底パス
	if !cfg.SwaggerEnabled {
		hc.DocsPath, hc.OpenAPIPath, hc.SchemasPath = "", "", ""
	}
	api := humago.New(mux, hc)
	health.Register(api, deps.Ready)
	return httpx.RequestID(httpx.Recover(deps.Logger, httpx.AccessLog(deps.Logger, mux))), api.OpenAPI()
}
