package migrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRunWithoutSQLDoesNotAccessDatabase(t *testing.T) {
	markerOnly := fstest.MapFS{
		"README.md": &fstest.MapFile{Data: []byte("migration marker")},
	}
	if err := Run(context.Background(), nil, markerOnly); err != nil {
		t.Fatalf("empty migrations: %v", err)
	}
}

func TestRunAppliesSQLWhenDatabaseAvailable(t *testing.T) {
	db := openTestDB(t)
	files := fstest.MapFS{
		"00001_smoke.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\nCREATE TABLE migrate_smoke (id int);\n-- +goose Down\nDROP TABLE migrate_smoke;\n")},
	}
	if err := Run(context.Background(), db, files); err != nil {
		t.Fatalf("適用: %v", err)
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM migrate_smoke").Scan(&n); err != nil {
		t.Fatalf("適用した表を読めない: %v", err)
	}
	if n != 0 {
		t.Fatalf("行数 = %d, 空の表が欲しい", n)
	}
	if err := Run(context.Background(), db, files); err != nil {
		t.Fatalf("2回目の適用は何もしない: %v", err)
	}
	version, err := Version(context.Background(), db)
	if err != nil {
		t.Fatalf("版: %v", err)
	}
	if version != 1 {
		t.Fatalf("版 = %d, 適用した 00001 が欲しい", version)
	}
}

func TestVersionReturnsZeroWhenVersionTableIsMissing(t *testing.T) {
	db := openTestDB(t)
	// goose は版の表が無いとき、表を作って 0 を返す。
	version, err := Version(context.Background(), db)
	if err != nil {
		t.Fatalf("版: %v", err)
	}
	if version != 0 {
		t.Fatalf("版 = %d, 未適用なら 0 が欲しい", version)
	}
}

func TestRunReturnsErrorWhenSQLIsInvalid(t *testing.T) {
	db := openTestDB(t)
	files := fstest.MapFS{
		"00001_bad.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\nTHIS IS NOT SQL;\n-- +goose Down\nSELECT 1;\n")},
	}
	err := Run(context.Background(), db, files)
	if err == nil || !strings.Contains(err.Error(), "00001_bad.sql") {
		t.Fatalf("不正なSQLは失敗する: %v", err)
	}
	version, vErr := Version(context.Background(), db)
	if vErr != nil {
		t.Fatalf("版: %v", vErr)
	}
	if version != 0 {
		t.Fatalf("版 = %d, 失敗した適用は版を進めない", version)
	}
}

// openTestDB は TEST_DATABASE_URL の接続先から、この試験だけの空のDBを作って開く。
// 接続先にある既存の表は消さない。URLが無いときと、一時DBを作る権限が無いときはスキップする。
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mtp_migrate_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		admin.Close()
		if cannotCreateDatabase(err) {
			t.Skip("一時DBを作る権限が無いためスキップ")
		}
		t.Fatalf("一時DBを作れません: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
		admin.Close()
	})
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// cannotCreateDatabase は、CREATE DATABASE が権限不足（SQLSTATE 42501）で失敗したかを返す。
func cannotCreateDatabase(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

func TestCannotCreateDatabase(t *testing.T) {
	if cannotCreateDatabase(nil) || cannotCreateDatabase(errors.New("permission denied")) {
		t.Fatal("SQLSTATE 42501 以外は権限不足にしない")
	}
	if !cannotCreateDatabase(fmt.Errorf("wrap: %w", &pgconn.PgError{Code: "42501"})) {
		t.Fatal("42501 は権限不足")
	}
	if cannotCreateDatabase(&pgconn.PgError{Code: "42P04"}) {
		t.Fatal("別の SQLSTATE は権限不足にしない")
	}
}

// globErrFS は一覧の取得に失敗する。標準の fs.Glob は ReadDir の失敗を握りつぶし、
// Glob を実装したファイルシステムが返したエラーだけを呼び出し元へ返す。
type globErrFS struct{ err error }

func (g globErrFS) Open(string) (fs.File, error) { return nil, g.err }

func (g globErrFS) Glob(string) ([]string, error) { return nil, g.err }

func TestRunReturnsErrorWhenMigrationFilesCannotBeRead(t *testing.T) {
	want := errors.New("migration files are unreadable")
	err := Run(context.Background(), nil, globErrFS{err: want})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want the filesystem error", err)
	}
}

func TestRunIgnoresSQLNestedInDirectories(t *testing.T) {
	nested := fstest.MapFS{
		"nested/00001_smoke.sql": &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n")},
		"notes.txt":              &fstest.MapFile{Data: []byte("not sql")},
	}
	if err := Run(context.Background(), nil, nested); err != nil {
		t.Fatalf("SQLが直下に無いときはDBへ触らず成功する: %v", err)
	}
}

func TestRunReturnsErrorWhenSQLFilenameHasNoVersion(t *testing.T) {
	for _, name := range []string{"readme.sql", "00000_zero.sql", "abc_name.sql"} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				name: &fstest.MapFile{Data: []byte("-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n")},
			}
			// ファイル名から版を読めない時点で失敗し、DBが無くてもパニックしない。
			err := Run(context.Background(), nil, files)
			if err == nil {
				t.Fatal("版の番号が読めないSQLは失敗する")
			}
		})
	}
}

// closedDB は接続しないまま閉じたDB。ネットワークには出ない。
func closedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", "postgres://migrate:migrate@127.0.0.1:1/migrate?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRunReturnsErrorWhenDatabaseIsClosed(t *testing.T) {
	files := fstest.MapFS{
		"00001_smoke.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\nCREATE TABLE migrate_smoke (id int);\n-- +goose Down\nDROP TABLE migrate_smoke;\n")},
	}
	if err := Run(context.Background(), closedDB(t), files); err == nil {
		t.Fatal("閉じたDBへは適用できない")
	}
}

func TestVersionReturnsErrorWhenDatabaseIsClosed(t *testing.T) {
	version, err := Version(context.Background(), closedDB(t))
	if err == nil {
		t.Fatalf("閉じたDBの版は失敗する: version=%d", version)
	}
}
