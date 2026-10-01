// Command api はMyTechPulseのGo APIサーバーを起動する。
// 起動時にDBマイグレーションは実行しない(cmd/migrateで明示的に実行する)。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/logging"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/postgres"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.LookupEnv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) error {
	cfg, err := config.Load(lookup)
	if err != nil {
		return err
	}
	logger := logging.New(stdout, slog.LevelInfo)
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	handler, _ := app.New(cfg, app.Dependencies{Logger: logger, Ready: pool})
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("%sで待ち受けできません", cfg.HTTPAddr)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("server started", "addr", ln.Addr().String(), "env", cfg.Environment)
	return serveUntilCanceled(ctx, server, ln, shutdownTimeout)
}

// serveUntilCanceled は ctx がキャンセルされるまで配信し、timeout以内に正常終了させる。
func serveUntilCanceled(ctx context.Context, server *http.Server, ln net.Listener, timeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(ln) }()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
