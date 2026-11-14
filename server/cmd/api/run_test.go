package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
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

// safeLog は run のログを、起動側と試験側の両方から読めるようにする。
type safeLog struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *safeLog) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeLog) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func serverAddrFromLog(logs string) (string, bool) {
	for _, line := range strings.Split(logs, "\n") {
		if !strings.Contains(line, `"msg":"server started"`) {
			continue
		}
		const key = `"addr":"`
		i := strings.Index(line, key)
		if i < 0 {
			return "", false
		}
		rest := line[i+len(key):]
		j := strings.IndexByte(rest, '"')
		if j <= 0 {
			return "", false
		}
		return rest[:j], true
	}
	return "", false
}

// startPostgresStandIn は、起動時の接続確認（"-- ping"）にだけ成功を返すローカルの受け口を返す。
// それ以外の問い合わせはエラーにする。本物の PostgreSQL も、Qiita・Zenn も使わない。
func startPostgresStandIn(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go servePostgresStandIn(conn)
		}
	}()
	return ln.Addr().String()
}

func standInDatabaseURL(addr string) string {
	return "postgres://u:p@" + addr + "/db?sslmode=disable&connect_timeout=2&pool_max_conns=1&default_query_exec_mode=simple_protocol"
}

func isPingQuery(body []byte) bool {
	return strings.TrimRight(string(body), "\x00") == "-- ping"
}

func servePostgresStandIn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	if err := readStartup(r, conn); err != nil {
		return
	}
	if err := writeReady(conn); err != nil {
		return
	}
	for {
		typ, body, err := readFrontend(r)
		if err != nil {
			return
		}
		switch typ {
		case 'X':
			return
		case 'Q':
			if isPingQuery(body) {
				// 空の成功。接続確認は通るが、行は返さない。
				if err := writeBackend(conn, 'I', nil); err != nil {
					return
				}
			} else if err := writeBackend(conn, 'E', standInError("unsupported")); err != nil {
				return
			}
			if err := writeBackend(conn, 'Z', []byte{'I'}); err != nil {
				return
			}
		case 'S':
			// 拡張問い合わせが来ても、同期の時点でエラーを返して待ち続けない。
			if err := writeBackend(conn, 'E', standInError("unsupported")); err != nil {
				return
			}
			if err := writeBackend(conn, 'Z', []byte{'I'}); err != nil {
				return
			}
		}
	}
}

func readStartup(r *bufio.Reader, w io.Writer) error {
	var n int32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return err
	}
	if n < 8 {
		return io.ErrUnexpectedEOF
	}
	body := make([]byte, n-4)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	if binary.BigEndian.Uint32(body[:4]) == 80877103 { // SSLRequest
		if _, err := w.Write([]byte{'N'}); err != nil {
			return err
		}
		return readStartup(r, w)
	}
	return nil
}

func writeReady(w io.Writer) error {
	if err := writeBackend(w, 'R', []byte{0, 0, 0, 0}); err != nil {
		return err
	}
	for _, param := range [][2]string{
		{"server_version", "17.0"},
		{"client_encoding", "UTF8"},
		{"DateStyle", "ISO, MDY"},
		{"integer_datetimes", "on"},
		{"TimeZone", "UTC"},
	} {
		payload := append(append([]byte(param[0]), 0), param[1]...)
		payload = append(payload, 0)
		if err := writeBackend(w, 'S', payload); err != nil {
			return err
		}
	}
	var key [8]byte
	binary.BigEndian.PutUint32(key[0:4], 1)
	binary.BigEndian.PutUint32(key[4:8], 2)
	if err := writeBackend(w, 'K', key[:]); err != nil {
		return err
	}
	return writeBackend(w, 'Z', []byte{'I'})
}

func standInError(msg string) []byte {
	var b []byte
	add := func(code byte, s string) {
		b = append(b, code)
		b = append(b, s...)
		b = append(b, 0)
	}
	add('S', "ERROR")
	add('V', "ERROR")
	add('C', "0A000")
	add('M', msg)
	return append(b, 0)
}

func writeBackend(w io.Writer, typ byte, payload []byte) error {
	var hdr [5]byte
	hdr[0] = typ
	binary.BigEndian.PutUint32(hdr[1:], uint32(4+len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readFrontend(r *bufio.Reader) (byte, []byte, error) {
	typ, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	var n int32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return 0, nil, err
	}
	if n < 4 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	body := make([]byte, n-4)
	_, err = io.ReadFull(r, body)
	return typ, body, err
}

func TestRunServesHealthAndStopsOnCancel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled string
	}{
		{name: "一括取得は無効", enabled: "false"},
		{name: "一括取得は有効", enabled: "true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := startPostgresStandIn(t)
			logs := &safeLog{}
			lookup := runLookup(map[string]string{
				"DATABASE_URL":          standInDatabaseURL(db),
				"HTTP_ADDR":             "127.0.0.1:0",
				"ARTICLE_FETCH_ENABLED": tc.enabled,
			})
			ctx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			var runErr error
			go func() {
				runErr = run(ctx, lookup, logs, io.Discard)
				close(stopped)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-stopped:
				case <-time.After(15 * time.Second):
					t.Errorf("run が止まらない")
				}
			})

			addr := waitForLive(t, logs, stopped, &runErr)
			ready, err := http.Get("http://" + addr + "/health/ready")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(ready.Body)
			ready.Body.Close()
			if ready.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
				t.Fatalf("ready = %d %s", ready.StatusCode, body)
			}

			cancel()
			select {
			case <-stopped:
				if runErr != nil {
					t.Fatalf("run = %v", runErr)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("停止が終わらない")
			}
		})
	}
}

func waitForLive(t *testing.T, logs *safeLog, stopped <-chan struct{}, runErr *error) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		select {
		case <-stopped:
			t.Fatalf("起動前に run が終了した: %v\n%s", *runErr, logs.String())
		default:
		}
		if addr, ok := serverAddrFromLog(logs.String()); ok {
			resp, err := http.Get("http://" + addr + "/health/live")
			if err == nil {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
					t.Fatalf("live = %d %s", resp.StatusCode, body)
				}
				return addr
			}
			last = err
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("10秒以内に起動しない: %v\n%s", last, logs.String())
	return ""
}

func TestRunFailsWhenListenFails(t *testing.T) {
	db := startPostgresStandIn(t)
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { busy.Close() })

	for _, tc := range []struct {
		name string
		addr string
	}{
		{name: "使われているポート", addr: busy.Addr().String()},
		{name: "範囲外のポート", addr: "127.0.0.1:99999"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookup := runLookup(map[string]string{
				"DATABASE_URL": standInDatabaseURL(db),
				"HTTP_ADDR":    tc.addr,
			})
			err := run(context.Background(), lookup, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.addr+"で待ち受けできません") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRunFailsWhenWebAuthnRejectsRPID(t *testing.T) {
	// 試験環境では IP アドレスの RP ID を設定の読み込みが通し、パスキーの組み立てで起動を止める。
	db := startPostgresStandIn(t)
	lookup := runLookup(map[string]string{
		"DATABASE_URL":     standInDatabaseURL(db),
		"WEBAUTHN_RP_ID":   "127.0.0.1",
		"WEBAUTHN_ORIGINS": "http://127.0.0.1",
	})
	err := run(context.Background(), lookup, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "WebAuthnの設定が不正です") {
		t.Fatalf("err = %v", err)
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
