package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenDoesNotReturnDatabaseURL(t *testing.T) {
	raw := "postgresql://secret-user:secret-pass@127.0.0.1:1/missing"
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := Open(ctx, raw)
	if err == nil || strings.Contains(err.Error(), "secret-pass") || strings.Contains(err.Error(), "secret-user") {
		t.Fatalf("unsafe error: %v", err)
	}
}

func TestOpenRejectsMalformedURLWithoutEchoingIt(t *testing.T) {
	_, err := Open(context.Background(), "postgresql://secret-user:secret-pass@127.0.0.1:notaport/db")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("unsafe error: %v", err)
	}
}
