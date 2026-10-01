// Package health はプロセス生存(live)とDB疎通(ready)の確認APIを提供する。
package health

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// ReadyChecker は依存先(DB)の疎通を確認する。
type ReadyChecker interface {
	Ping(context.Context) error
}

// LiveOutput は live 成功時の応答。
type LiveOutput struct {
	Body struct {
		Status string `json:"status" enum:"ok" doc:"正常時は常にok"`
	}
}

// ReadyOutput は ready 成功時の応答。
type ReadyOutput struct {
	Body struct {
		Status string `json:"status" enum:"ok" doc:"正常時は常にok"`
	}
}

// Register は /health/live と /health/ready を登録する。
func Register(api huma.API, checker ReadyChecker) {
	huma.Register(api, huma.Operation{
		OperationID: "health-live",
		Method:      http.MethodGet,
		Path:        "/health/live",
		Summary:     "プロセスの生存確認",
		Tags:        []string{"health"},
	}, func(context.Context, *struct{}) (*LiveOutput, error) {
		out := &LiveOutput{}
		out.Body.Status = "ok"
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "health-ready",
		Method:      http.MethodGet,
		Path:        "/health/ready",
		Summary:     "DB疎通の確認",
		Tags:        []string{"health"},
		Errors:      []int{http.StatusServiceUnavailable},
	}, func(ctx context.Context, _ *struct{}) (*ReadyOutput, error) {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := checker.Ping(pingCtx); err != nil {
			// 接続情報を含み得るため、原因は応答に載せない
			return nil, huma.Error503ServiceUnavailable("データベースへ接続できません")
		}
		out := &ReadyOutput{}
		out.Body.Status = "ok"
		return out, nil
	})
}
