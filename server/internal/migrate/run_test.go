package migrate

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

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
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS migrate_smoke; DROP TABLE IF EXISTS goose_db_version")
	})
	files := fstest.MapFS{
		"00001_smoke.sql": &fstest.MapFile{Data: []byte(
			"-- +goose Up\nCREATE TABLE migrate_smoke (id int);\n-- +goose Down\nDROP TABLE migrate_smoke;\n")},
	}
	if err := Run(context.Background(), db, files); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := Run(context.Background(), db, files); err != nil {
		t.Fatalf("rerun should be idempotent: %v", err)
	}
	version, err := Version(context.Background(), db)
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if version < 1 {
		t.Fatalf("version = %d, want the applied migration", version)
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
