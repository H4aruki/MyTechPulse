package main

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// 接続せずにすぐ形式エラーになるURL。ポートが数字ではない。
const invalidDatabaseURL = "postgres://localhost:notaport/db"

func TestRunHelpWritesUsageAndSkipsDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", invalidDatabaseURL)
	for _, arg := range []string{"-help", "--help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run([]string{"-version", arg}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			if stdout.String() != usageText+"\n" {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunShortHelpWritesUsageToStderr(t *testing.T) {
	t.Setenv("DATABASE_URL", invalidDatabaseURL)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-h"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != usageText+"\n" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunUnknownFlagExits2(t *testing.T) {
	t.Setenv("DATABASE_URL", invalidDatabaseURL)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-unknown"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -unknown") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if strings.Count(stderr.String(), usageText) != 1 {
		t.Fatalf("使い方が1回ではない: %q", stderr.String())
	}
}

func TestRunRejectsInvalidBoolean(t *testing.T) {
	t.Setenv("DATABASE_URL", invalidDatabaseURL)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-version=maybe"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code = %d, stderr = %s", code, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "invalid boolean value") || strings.Count(stderr.String(), usageText) != 1 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunRequiresDatabaseURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		url  string
	}{
		{name: "空文字", url: ""},
		{name: "空白だけ", url: " \t\n "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", tc.url)
			var stdout, stderr bytes.Buffer
			code := run(nil, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if stderr.String() != "migrate: DATABASE_URLが未設定です\n" {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunVersionInvalidURL(t *testing.T) {
	t.Setenv("DATABASE_URL", invalidDatabaseURL)
	for _, args := range [][]string{{"-version"}, {"--version"}, {"-version", "extra"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if stderr.String() != "migrate: DBの版を取得できません\n" {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunMigrateInvalidURL(t *testing.T) {
	t.Setenv("DATABASE_URL", invalidDatabaseURL)
	for _, args := range [][]string{nil, {"-version=false"}} {
		name := "適用"
		if args != nil {
			name = strings.Join(args, " ")
		}
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(args, &stdout, &stderr)
			if code != 1 {
				t.Fatalf("code = %d, stderr = %s", code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout.String())
			}
			if !strings.HasPrefix(stderr.String(), "migrate: マイグレーションに失敗しました:") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunAppliesMigrationsAndPrintsVersion(t *testing.T) {
	dbURL := testDatabaseURL(t)
	t.Setenv("DATABASE_URL", dbURL)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"-version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("適用前の版の終了コードが %d。標準エラー: %s", code, hideDatabaseURL(stderr.String(), dbURL))
	}
	if strings.TrimSpace(stdout.String()) != "0" {
		t.Fatalf("適用前の版 = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("適用の終了コードが %d。標準エラー: %s", code, hideDatabaseURL(stderr.String(), dbURL))
	}
	if stdout.Len() != 0 {
		t.Fatalf("適用時の標準出力 = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"-version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("適用後の版の終了コードが %d。標準エラー: %s", code, hideDatabaseURL(stderr.String(), dbURL))
	}
	after := strings.TrimSpace(stdout.String())
	if after == "" || after == "0" {
		t.Fatalf("適用後の版 = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("2回目の適用の終了コードが %d。標準エラー: %s", code, hideDatabaseURL(stderr.String(), dbURL))
	}
	stdout.Reset()
	if code := run([]string{"-version"}, &stdout, &stderr); code != 0 {
		t.Fatalf("2回目の版の終了コードが %d。標準エラー: %s", code, hideDatabaseURL(stderr.String(), dbURL))
	}
	if strings.TrimSpace(stdout.String()) != after {
		t.Fatalf("版が変わった: %s -> %q", after, stdout.String())
	}
}

// testDatabaseURL は TEST_DATABASE_URL から、この試験だけの空のDBを作ってそのURLを返す。
// 無ければスキップする。既存のDBにはマイグレーションを適用しない。
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(hideDatabaseURL(err.Error(), raw))
	}
	var nonce [4]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	name := fmt.Sprintf("mtp_cmdmigrate_%d_%s", os.Getpid(), hex.EncodeToString(nonce[:]))
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		admin.Close()
		t.Fatal(hideDatabaseURL("テスト用DBを作れません: "+err.Error(), raw))
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
		admin.Close()
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(hideDatabaseURL(err.Error(), raw))
	}
	u.Path = "/" + name
	return u.String()
}

// hideDatabaseURL は失敗メッセージへ接続文字列と、その中のパスワードを出さない。
func hideDatabaseURL(message, databaseURL string) string {
	if databaseURL == "" {
		return message
	}
	message = strings.ReplaceAll(message, databaseURL, "[DATABASE_URL]")
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.User == nil {
		return message
	}
	if password, ok := parsed.User.Password(); ok && password != "" {
		message = strings.ReplaceAll(message, password, "[password]")
	}
	if userInfo := parsed.User.String(); userInfo != "" {
		message = strings.ReplaceAll(message, userInfo, "[user]")
	}
	return message
}
