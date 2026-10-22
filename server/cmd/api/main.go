// Command api はMyTechPulseのGo APIサーバーを起動する。
// 起動時にDBマイグレーションは実行しない(cmd/migrateで明示的に実行する)。
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/app"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/logging"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/postgres"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/qiita"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/zenn"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
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
	fetcher := provider.NewJSONFetcher(cfg.ProviderTimeout, cfg.ProviderMaxBytes)
	recommendService := &recommendation.Service{
		Interests:   store.NewInterest(pool),
		Providers:   recommendation.ProviderSet{Qiita: qiita.New(fetcher, cfg.QiitaToken), Zenn: zenn.New(fetcher)},
		FeedTimeout: cfg.FeedTimeout,
	}
	feedbackService := &userfeedback.Service{Repo: store.NewFeedback(pool), Clock: auth.SystemClock{}, Logger: logger}
	// パスワードとパスキーで、同じ制限器を共有する(失敗を合算して数える)
	passkeyService, err := newPasskeyService(cfg, pool, logger, authService.Limiter)
	if err != nil {
		return err
	}
	handler, _ := app.New(cfg, app.Dependencies{Logger: logger, Ready: pool, Auth: authService, Recommendation: recommendService, UserFeedback: feedbackService, Passkey: passkeyService})
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("%sで待ち受けできません", cfg.HTTPAddr)
	}
	// 記事の定期一括取得(既定は無効)。pool.Close より先に止めるため、defer で後に登録する。
	stopArticleFetch := startArticleFetch(ctx, cfg, pool, logger, shutdownTimeout)
	defer stopArticleFetch()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	logger.Info("server started", "addr", ln.Addr().String(), "env", cfg.Environment)
	return serveUntilCanceled(ctx, server, ln, shutdownTimeout)
}

// startArticleFetch は、ARTICLE_FETCH_ENABLED=true のときだけ、記事の定期一括取得を別のゴルーチンで起動する。
// 無効なら何も起動せず、何もしない stop を返す。返した stop は、取得を止めて、timeout 以内に終わるのを待つ。
// 取得には、再試行を自分で持つため JSONFetcher の内部の再試行を切った(MaxAttempts=1)部品を使う。
func startArticleFetch(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger, timeout time.Duration) (stop func()) {
	if !cfg.ArticleFetch.Enabled {
		return func() {}
	}
	fetcher := provider.NewJSONFetcher(cfg.ProviderTimeout, cfg.ProviderMaxBytes)
	fetcher.MaxAttempts = 1
	repo := store.NewArticleFetch(pool)
	clock := articlefetch.SystemClock{}
	fetchLogger := logger.With("component", "articlefetch")
	scheduler := &articlefetch.Scheduler{
		Runs:   repo,
		Locker: repo,
		Job: &articlefetch.Job{
			Store:            repo,
			Qiita:            qiita.New(fetcher, cfg.QiitaToken),
			Zenn:             zenn.New(fetcher),
			Clock:            clock,
			Logger:           fetchLogger,
			QiitaMaxRequests: cfg.ArticleFetch.QiitaMaxRequests,
			Jitter:           func() float64 { return rand.Float64()*2 - 1 },
		},
		Clock:      clock,
		Logger:     fetchLogger,
		RunTimeout: cfg.ArticleFetch.RunTimeout,
	}
	fetchCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Run(fetchCtx)
	}()
	logger.Info("article fetch enabled", "qiita_max_requests", cfg.ArticleFetch.QiitaMaxRequests, "run_timeout", cfg.ArticleFetch.RunTimeout.String())
	return func() {
		cancel()
		select {
		case <-done:
		case <-time.After(timeout):
			logger.Warn("article fetch did not stop in time")
		}
	}
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
		Limiter:    auth.NewLoginLimiter(auth.SystemClock{}),
	}, nil
}

// newPasskeyService はパスキーの実依存を組み立てる。RP IDとオリジンが不正なら起動を止める。
func newPasskeyService(cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger, limiter passkey.IPLimiter) (*passkey.Service, error) {
	w, err := passkey.NewWebAuthn(passkey.Config{RPID: cfg.WebAuthnRPID, RPDisplayName: cfg.WebAuthnRPName, Origins: cfg.WebAuthnOrigins})
	if err != nil {
		return nil, err
	}
	return &passkey.Service{
		WebAuthn:   w,
		Passkeys:   store.NewPasskey(pool),
		Challenges: store.NewPasskeyChallenge(pool),
		Clock:      auth.SystemClock{},
		Logger:     logger,
		Limiter:    limiter,
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
