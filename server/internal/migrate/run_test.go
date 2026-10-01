package migrate

import (
	"context"
	"database/sql"
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
}
