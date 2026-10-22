package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/platform/config"
)

func TestServeUntilCanceledShutsDownGracefully(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveUntilCanceled(ctx, server, ln, 10*time.Second) }()

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveUntilCanceled = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not finish within 10s")
	}
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	if resp, err := client.Get("http://" + addr + "/"); err == nil {
		resp.Body.Close()
		t.Fatal("new connection accepted after shutdown")
	}
}

func TestRunFailsFastOnInvalidConfig(t *testing.T) {
	lookup := func(string) (string, bool) { return "", false }
	if err := run(context.Background(), lookup, nil, nil); err == nil {
		t.Fatal("expected config error")
	}
}

func TestStartArticleFetchDoesNothingWhenDisabled(t *testing.T) {
	var cfg config.Config // ArticleFetch.Enabled は false
	before := runtime.NumGoroutine()
	// 無効のときはDBに触れない(プールがnilでも落ちない)
	stop := startArticleFetch(context.Background(), cfg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
	if stop == nil {
		t.Fatal("stop が nil")
	}
	stop()
	time.Sleep(20 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("ゴルーチンが増えた: %d -> %d", before, after)
	}
}

func TestStartArticleFetchStopsOnShutdown(t *testing.T) {
	// 接続できないDBでも、起動して、止めれば速やかに終わる(Qiita・Zennへは通信しない)。
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cfg := config.Config{ArticleFetch: config.ArticleFetch{Enabled: true, QiitaMaxRequests: 300, RunTimeout: time.Minute}}
	stop := startArticleFetch(context.Background(), cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)), 5*time.Second)
	done := make(chan struct{})
	go func() { stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("停止が終わらない")
	}
}
