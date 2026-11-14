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
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
	"github.com/H4aruki/MyTechPulse/server/internal/savedarticle"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

const specPath = "openapi/openapi.json"

type noopChecker struct{}

func (noopChecker) Ping(context.Context) error { return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run は仕様を openapi/openapi.json へ書き、終了コードを返す。
// 引数と標準出力は使わない。出力先を引数で変えると、いまのコマンドの動きが変わるため。
func run(args []string, stdout, stderr io.Writer) int {
	_ = args
	_ = stdout
	if err := writeSpec(specPath); err != nil {
		fmt.Fprintln(stderr, "openapi:", err)
		return 1
	}
	return 0
}

// writeSpec は OpenAPI 仕様を path へ書く。親ディレクトリが無ければ作る。
func writeSpec(path string) error {
	// 仕様の生成だけなので、認証Serviceの依存は空でよい(経路は呼ばれない)。
	_, spec := app.New(config.Config{Environment: "local", SwaggerEnabled: true, CookieName: "mtp_session"}, app.Dependencies{
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Ready:          noopChecker{},
		Auth:           &auth.Service{},
		Recommendation: &recommendation.Service{},
		UserFeedback:   &userfeedback.Service{},
		Passkey:        &passkey.Service{},
		SavedArticle:   &savedarticle.Service{},
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
