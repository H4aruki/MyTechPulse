package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/passkey"
	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

const unreachableDatabaseURL = "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1"

// runLookup は run に渡す設定を map から作る。外部(Qiita・Zenn)へは通信しない(一括取得は無効のまま)。
func runLookup(overrides map[string]string) func(string) (string, bool) {
	env := map[string]string{
		"APP_ENV":            "test",
		"QIITA_ACCESS_TOKEN": "synthetic-token",
	}
	for k, v := range overrides {
		env[k] = v
	}
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), unreachableDatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestRunRejectsMalformedDatabaseURL(t *testing.T) {
	lookup := runLookup(map[string]string{"DATABASE_URL": "postgres://u:p@127.0.0.1:notaport/db"})
	err := run(context.Background(), lookup, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URLの形式が不正です") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunFailsWhenDatabaseUnreachable(t *testing.T) {
	lookup := runLookup(map[string]string{"DATABASE_URL": unreachableDatabaseURL})
	done := make(chan error, 1)
	go func() { done <- run(context.Background(), lookup, io.Discard, io.Discard) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "DBへ接続できません") {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run が数秒以内に返らない")
	}
}

func TestNewAuthService(t *testing.T) {
	cfg := config.Config{SessionTTL: 36 * time.Hour}
	svc, err := newAuthService(cfg, lazyPool(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if len(svc.DummyHash) == 0 {
		t.Error("DummyHash が空")
	}
	if svc.SessionTTL != cfg.SessionTTL {
		t.Errorf("SessionTTL = %v", svc.SessionTTL)
	}
	if svc.Limiter == nil {
		t.Error("Limiter が nil")
	}
}

type stubIPLimiter struct{}

func (stubIPLimiter) AllowBegin(string) error { return nil }
func (stubIPLimiter) CheckIP(string) error    { return nil }
func (stubIPLimiter) FailureIP(string)        {}

func TestNewPasskeyService(t *testing.T) {
	pool := lazyPool(t)
	valid := config.Config{WebAuthnRPID: "example.com", WebAuthnRPName: "MyTechPulse", WebAuthnOrigins: []string{"https://example.com"}}

	t.Run("正しい設定", func(t *testing.T) {
		limiter := stubIPLimiter{}
		svc, err := newPasskeyService(valid, pool, testLogger(), limiter)
		if err != nil {
			t.Fatal(err)
		}
		if svc.WebAuthn == nil {
			t.Error("WebAuthn が nil")
		}
		if svc.Limiter != passkey.IPLimiter(limiter) {
			t.Error("渡した Limiter が入っていない")
		}
	})
	t.Run("authのLimiterを渡せる", func(t *testing.T) {
		if _, err := newPasskeyService(valid, pool, testLogger(), auth.NewLoginLimiter(auth.SystemClock{})); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("オリジンが空", func(t *testing.T) {
		cfg := valid
		cfg.WebAuthnOrigins = nil
		if _, err := newPasskeyService(cfg, pool, testLogger(), stubIPLimiter{}); err == nil {
			t.Fatal("エラーになるはず")
		}
	})
}

func TestServeUntilCanceledReturnsServeError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	err = serveUntilCanceled(context.Background(), &http.Server{}, ln, time.Second)
	if err == nil {
		t.Fatal("Serve のエラーが返らない")
	}
}

// requireTestDatabase は TEST_DATABASE_URL を返す。無ければスキップする。run は起動時にDBへ接続確認するが、書き込みはしない。
func requireTestDatabase(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	return raw
}

func TestRunServesAndStopsOnCancel(t *testing.T) {
	dbURL := requireTestDatabase(t)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()

	lookup := runLookup(map[string]string{"DATABASE_URL": dbURL, "HTTP_ADDR": addr})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, lookup, io.Discard, io.Discard) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/health/live")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d", resp.StatusCode)
			}
			break
		}
		select {
		case err := <-done:
			t.Fatalf("起動前に run が終了した: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("10秒以内に起動しない: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run = %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("停止が終わらない")
	}
}

func TestRunFailsWhenAddressInUse(t *testing.T) {
	dbURL := requireTestDatabase(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	lookup := runLookup(map[string]string{"DATABASE_URL": dbURL, "HTTP_ADDR": busy.Addr().String()})
	err = run(context.Background(), lookup, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "で待ち受けできません") {
		t.Fatalf("err = %v", err)
	}
}
