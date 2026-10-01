package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
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
