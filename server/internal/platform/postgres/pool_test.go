package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenDoesNotReturnDatabaseURL(t *testing.T) {
	raw := "postgresql://secret-user:secret-pass@127.0.0.1:1/missing"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	requireOpenError(t, raw, ctx, "DBへ接続できません")
}

func TestOpenRejectsMalformedURLWithoutEchoingIt(t *testing.T) {
	raw := "postgresql://secret-user:secret-pass@127.0.0.1:notaport/db"
	requireOpenError(t, raw, context.Background(), "DATABASE_URLの形式が不正です")
}

// pool_max_conns=0 は「DB接続を作成できません」ではなく、URLの形式エラーになる。
// 今の pgx では、Open からプール作成失敗には届かない。ParseConfig が
// 1 未満のプールサイズを形式エラーにし、NewWithConfig が失敗するのも
// そのサイズのときだけだから。
func TestOpenRejectsNonPositivePoolSizeAsMalformedURL(t *testing.T) {
	raw := "postgresql://secret-user:secret-pass@127.0.0.1:1/db?pool_max_conns=0"
	requireOpenError(t, raw, context.Background(), "DATABASE_URLの形式が不正です")
}

func TestOpenCanceledContextIsConnectError(t *testing.T) {
	raw := "postgresql://secret-user:secret-pass@127.0.0.1:1/missing"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requireOpenError(t, raw, ctx, "DBへ接続できません")
}

func TestOpenReturnsPoolWhenDatabaseIsAvailable(t *testing.T) {
	raw := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := Open(ctx, raw)
	if err != nil {
		if leaksConnectionString(err, raw) {
			t.Fatal("エラー本文に接続文字列が含まれる")
		}
		t.Fatalf("プールを返せなかった: %s", err.Error())
	}
	t.Cleanup(pool.Close)

	var n int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
		t.Fatal("プールは返ったが、問い合わせに失敗した")
	}
	if n != 1 {
		t.Fatalf("SELECT 1 = %d", n)
	}
}

func TestOpenWrongPasswordIsConnectError(t *testing.T) {
	raw := testDatabaseURL(t)
	bad, err := urlWithPassword(raw, "secret-pass")
	if err != nil {
		t.Fatal("試験用のURLを組み立てられなかった")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	requireOpenError(t, bad, ctx, "DBへ接続できません")
}

func TestOpenMissingDatabaseIsConnectError(t *testing.T) {
	raw := testDatabaseURL(t)
	bad, err := urlWithDatabase(raw, "mtp_postgres_open_missing")
	if err != nil {
		t.Fatal("試験用のURLを組み立てられなかった")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	requireOpenError(t, bad, ctx, "DBへ接続できません")
}

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	return raw
}

func requireOpenError(t *testing.T, raw string, ctx context.Context, want string) {
	t.Helper()
	_, err := Open(ctx, raw)
	if err == nil {
		t.Fatal("エラーを期待した")
	}
	if leaksConnectionString(err, raw) {
		t.Fatal("エラー本文に接続文字列が含まれる")
	}
	if err.Error() != want {
		t.Fatalf("エラーの種類が違う: %q", err.Error())
	}
}

func leaksConnectionString(err error, raw string) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if raw != "" && strings.Contains(msg, raw) {
		return true
	}
	u, parseErr := url.Parse(raw)
	if parseErr != nil || u.User == nil {
		return strings.Contains(msg, "secret-pass") || strings.Contains(msg, "secret-user")
	}
	if user := u.User.Username(); user != "" && strings.Contains(msg, user) {
		return true
	}
	pass, ok := u.User.Password()
	return ok && pass != "" && strings.Contains(msg, pass)
}

func urlWithPassword(raw, pass string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	user := ""
	if u.User != nil {
		user = u.User.Username()
	}
	u.User = url.UserPassword(user, pass)
	return u.String(), nil
}

func urlWithDatabase(raw, name string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}
