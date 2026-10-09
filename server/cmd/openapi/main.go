// Command openapi はDB接続なしでOpenAPI仕様を生成し、openapi/openapi.json へ書き出す。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

type noopChecker struct{}

func (noopChecker) Ping(context.Context) error { return nil }

func main() {
	if err := run("openapi/openapi.json"); err != nil {
		fmt.Fprintln(os.Stderr, "openapi:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	// 仕様の生成だけなので、認証Serviceの依存は空でよい(経路は呼ばれない)。
	_, spec := app.New(config.Config{Environment: "local", SwaggerEnabled: true, CookieName: "mtp_session"}, app.Dependencies{
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Ready:          noopChecker{},
		Auth:           &auth.Service{},
		Recommendation: &recommendation.Service{},
		UserFeedback:   &userfeedback.Service{},
	})
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
