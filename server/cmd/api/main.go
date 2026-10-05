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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/logging"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/postgres"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
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
	authService, err := newAuthService(cfg, pool, logger)
	if err != nil {
		return err
	}
	handler, _ := app.New(cfg, app.Dependencies{Logger: logger, Ready: pool, Auth: authService})
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("%sで待ち受けできません", cfg.HTTPAddr)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("server started", "addr", ln.Addr().String(), "env", cfg.Environment)
	return serveUntilCanceled(ctx, server, ln, shutdownTimeout)
}

// newAuthService は認証の実依存を組み立てる。
// 存在しない利用者のログインでも同じ照合処理を行うための固定ハッシュは、起動時に1度だけ作る。
func newAuthService(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*auth.Service, error) {
	passwords := auth.BcryptPasswords{}
	dummy, err := auth.NewDummyHash(passwords)
	if err != nil {
		return nil, err
	}
	repo := store.NewAuth(pool)
	return &auth.Service{
		Users:      repo,
		Sessions:   repo,
		Passwords:  passwords,
		Tokens:     auth.RandomTokenGenerator{},
		Clock:      auth.SystemClock{},
		SessionTTL: cfg.SessionTTL,
		DummyHash:  dummy,
		Logger:     logger,
	}, nil
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
