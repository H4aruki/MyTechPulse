package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
)

// newIsolatedDatabase はテスト専用の空DBを作る。既存DBには触れず、作ったDBだけを後始末する。
func newIsolatedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mtp_migtest_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
		admin.Close()
	})
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// createLegacyCompatibleSchema は現行Python版が作る3表を手動で再現する。
func createLegacyCompatibleSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE "user" (
		"user_ID" serial PRIMARY KEY,
		user_name varchar(50) NOT NULL UNIQUE,
		password varchar(255) NOT NULL)`)
	mustExec(t, db, `CREATE TABLE tag (
		"tag_ID" serial PRIMARY KEY,
		tag_name varchar(50) NOT NULL UNIQUE)`)
	mustExec(t, db, `CREATE TABLE recommend (
		"user_ID" integer NOT NULL REFERENCES "user"("user_ID") ON DELETE CASCADE,
		"tag_ID" integer NOT NULL REFERENCES tag("tag_ID") ON DELETE CASCADE,
		match_int integer NOT NULL,
		PRIMARY KEY ("user_ID", "tag_ID"))`)
}

func assertLegacySchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if n := count(t, db, `SELECT count(*) FROM information_schema.tables
		WHERE table_schema='public' AND table_name IN ('user','tag','recommend')`); n != 3 {
		t.Fatalf("expected 3 legacy tables, got %d", n)
	}
	if n := count(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id = 1 AND is_applied`); n != 1 {
		t.Fatalf("goose version 1 must be recorded once, got %d", n)
	}
}

func TestLegacyBaselineCreatesEmptyDatabase(t *testing.T) {
	db := newIsolatedDatabase(t)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("migrate empty database: %v", err)
	}
	assertLegacySchema(t, db)
	// 新規作成したスキーマでも、自動採番とCASCADEが動くこと
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('a', 'h')`)
	mustExec(t, db, `INSERT INTO tag(tag_name) VALUES ('Go')`)
	mustExec(t, db, `INSERT INTO recommend VALUES (1, 1, 5000)`)
	mustExec(t, db, `DELETE FROM "user" WHERE "user_ID" = 1`)
	if n := count(t, db, `SELECT count(*) FROM recommend`); n != 0 {
		t.Fatalf("recommend must cascade on user delete, got %d rows", n)
	}
}

func TestLegacyBaselineAcceptsCompatibleExistingDatabase(t *testing.T) {
	db := newIsolatedDatabase(t)
	createLegacyCompatibleSchema(t, db)
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('alice', '$2b$12$synthetic')`)
	mustExec(t, db, `INSERT INTO tag(tag_name) VALUES ('Python')`)
	mustExec(t, db, `INSERT INTO recommend VALUES (1, 1, 2000)`)
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("migrate compatible existing database: %v", err)
	}
	assertLegacySchema(t, db)
	var hash string
	if err := db.QueryRow(`SELECT password FROM "user" WHERE user_name='alice'`).Scan(&hash); err != nil || hash != "$2b$12$synthetic" {
		t.Fatalf("existing data must be preserved: %q %v", hash, err)
	}
	if n := count(t, db, `SELECT match_int FROM recommend`); n != 2000 {
		t.Fatalf("match_int must be preserved, got %d", n)
	}
}

func TestLegacyBaselineRejectsPartialExistingUserTableBeforeDDL(t *testing.T) {
	db := newIsolatedDatabase(t)
	mustExec(t, db, `CREATE TABLE "user" ("user_ID" text PRIMARY KEY)`)
	err := migrate.Run(context.Background(), db, migrations.FS)
	if err == nil || !strings.Contains(err.Error(), "legacy schema mismatch") {
		t.Fatalf("expected schema mismatch, got %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("expected pre-DDL schema mismatch, got %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('tag','recommend')`); n != 0 {
		t.Fatalf("no DDL may run on mismatch, found %d new tables", n)
	}
}

func TestLegacyBaselineRejectsIncompatibleColumnsAndConstraints(t *testing.T) {
	cases := map[string]func(*testing.T, *sql.DB){
		"password too short": func(t *testing.T, db *sql.DB) {
			mustExec(t, db, `ALTER TABLE "user" ALTER COLUMN password TYPE varchar(100)`)
		},
		"extra column": func(t *testing.T, db *sql.DB) {
			mustExec(t, db, `ALTER TABLE tag ADD COLUMN extra integer`)
		},
		"missing cascade": func(t *testing.T, db *sql.DB) {
			mustExec(t, db, `ALTER TABLE recommend DROP CONSTRAINT "recommend_tag_ID_fkey"`)
			mustExec(t, db, `ALTER TABLE recommend ADD FOREIGN KEY ("tag_ID") REFERENCES tag("tag_ID")`)
		},
		"missing unique": func(t *testing.T, db *sql.DB) {
			mustExec(t, db, `ALTER TABLE "user" DROP CONSTRAINT user_user_name_key`)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			db := newIsolatedDatabase(t)
			createLegacyCompatibleSchema(t, db)
			mutate(t, db)
			err := migrate.Run(context.Background(), db, migrations.FS)
			if err == nil || !strings.Contains(err.Error(), "legacy schema mismatch") {
				t.Fatalf("expected schema mismatch, got %v", err)
			}
		})
	}
}
