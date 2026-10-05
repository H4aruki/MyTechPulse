package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
)

func applyAll(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := migrate.Run(context.Background(), db, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}

func assertColumn(t *testing.T, db *sql.DB, table, column, dataType string, nullable bool) {
	t.Helper()
	var gotType, gotNullable string
	err := db.QueryRow(`SELECT data_type, is_nullable FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND column_name=$2`, table, column).Scan(&gotType, &gotNullable)
	if err != nil {
		t.Fatalf("column %s.%s not found: %v", table, column, err)
	}
	want := "NO"
	if nullable {
		want = "YES"
	}
	if gotType != dataType || gotNullable != want {
		t.Fatalf("column %s.%s = %s/%s, want %s/%s", table, column, gotType, gotNullable, dataType, want)
	}
}

func assertCheckConstraint(t *testing.T, db *sql.DB, table, name string) {
	t.Helper()
	if n := count(t, db, fmt.Sprintf(`SELECT count(*) FROM pg_constraint
		WHERE contype='c' AND conname='%s' AND conrelid = to_regclass('public."%s"')`, name, table)); n != 1 {
		t.Fatalf("check constraint %s on %s not found", name, table)
	}
}

// assertForeignKey は table.column が refTable.refColumn を参照し、削除時の動作が onDelete であることを確認する。
func assertForeignKey(t *testing.T, db *sql.DB, table, column, refTable, refColumn, onDelete string) {
	t.Helper()
	var action string
	err := db.QueryRow(`SELECT CASE c.confdeltype WHEN 'c' THEN 'CASCADE' WHEN 'a' THEN 'NO ACTION'
			WHEN 'r' THEN 'RESTRICT' WHEN 'n' THEN 'SET NULL' ELSE c.confdeltype::text END
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		JOIN pg_attribute ra ON ra.attrelid = c.confrelid AND ra.attnum = c.confkey[1]
		WHERE c.contype='f' AND c.conrelid = to_regclass('public."'||$1||'"')
		  AND c.confrelid = to_regclass('public."'||$2||'"')
		  AND a.attname=$3 AND ra.attname=$4`, table, refTable, column, refColumn).Scan(&action)
	if err != nil {
		t.Fatalf("foreign key %s.%s -> %s.%s not found: %v", table, column, refTable, refColumn, err)
	}
	if action != onDelete {
		t.Fatalf("foreign key on delete = %s, want %s", action, onDelete)
	}
}

func assertIndex(t *testing.T, db *sql.DB, table, name string) {
	t.Helper()
	if n := count(t, db, fmt.Sprintf(`SELECT count(*) FROM pg_indexes
		WHERE schemaname='public' AND tablename='%s' AND indexname='%s'`, table, name)); n != 1 {
		t.Fatalf("index %s on %s not found", name, table)
	}
}

func TestAuthSchema(t *testing.T) {
	db := newIsolatedDatabase(t)
	applyAll(t, db)
	assertColumn(t, db, `user`, `role`, `character varying`, false)
	assertCheckConstraint(t, db, `user`, `user_role_check`)
	assertForeignKey(t, db, `auth_session`, `user_ID`, `user`, `user_ID`, `CASCADE`)
	assertIndex(t, db, `auth_session`, `auth_session_expires_at_idx`)
	assertIndex(t, db, `auth_session`, `auth_session_user_id_idx`)
	assertColumn(t, db, `auth_session`, `token_hash`, `bytea`, false)
}

func TestAuthSchemaConstraintsAndCascade(t *testing.T) {
	db := newIsolatedDatabase(t)
	applyAll(t, db)
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('a', 'h')`)
	var role string
	if err := db.QueryRow(`SELECT role FROM "user" WHERE user_name='a'`).Scan(&role); err != nil || role != "member" {
		t.Fatalf("default role must be member: %q %v", role, err)
	}
	if _, err := db.Exec(`INSERT INTO "user"(user_name, password, role) VALUES ('b', 'h', 'root')`); err == nil {
		t.Fatal("role outside member/admin must be rejected")
	}
	mustExec(t, db, `INSERT INTO "user"(user_name, password, role) VALUES ('c', 'h', 'admin')`)
	if _, err := db.Exec(`INSERT INTO auth_session(token_hash, "user_ID", expires_at) VALUES ('\x00', 1, now())`); err == nil {
		t.Fatal("token_hash that is not 32 bytes must be rejected")
	}
	mustExec(t, db, `INSERT INTO auth_session(token_hash, "user_ID", expires_at)
		VALUES (decode(repeat('ab', 32), 'hex'), 1, now() + interval '1 hour')`)
	mustExec(t, db, `DELETE FROM "user" WHERE "user_ID" = 1`)
	if n := count(t, db, `SELECT count(*) FROM auth_session`); n != 0 {
		t.Fatalf("sessions must cascade on user delete, got %d", n)
	}
}

func TestAuthMigrationsPreserveExistingUsers(t *testing.T) {
	db := newIsolatedDatabase(t)
	createLegacyCompatibleSchema(t, db)
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('alice', '$2b$12$synthetic')`)
	applyAll(t, db)
	var hash, role string
	if err := db.QueryRow(`SELECT password, role FROM "user" WHERE user_name='alice'`).Scan(&hash, &role); err != nil {
		t.Fatal(err)
	}
	if hash != "$2b$12$synthetic" || role != "member" {
		t.Fatalf("existing user must keep password and become member: %q %q", hash, role)
	}
	if n := count(t, db, `SELECT count(*) FROM "user"`); n != 1 {
		t.Fatalf("user count = %d", n)
	}
}

// downは本番の切り戻しには使わない。開発用に、追加物だけが外れ既存3表が残ることを確認する。
func TestAuthMigrationsDownKeepsLegacyTables(t *testing.T) {
	db := newIsolatedDatabase(t)
	applyAll(t, db)
	mustExec(t, db, `INSERT INTO "user"(user_name, password) VALUES ('a', 'h')`)
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := goose.DownContext(ctx, db, "."); err != nil { // 00003
		t.Fatalf("down 00003: %v", err)
	}
	if err := goose.DownContext(ctx, db, "."); err != nil { // 00002
		t.Fatalf("down 00002: %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='auth_session'`); n != 0 {
		t.Fatal("auth_session must be dropped by down")
	}
	if n := count(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name='user' AND column_name='role'`); n != 0 {
		t.Fatal("role must be dropped by down")
	}
	if n := count(t, db, `SELECT count(*) FROM "user"`); n != 1 {
		t.Fatalf("existing users must remain, got %d", n)
	}
	// 再適用できること
	applyAll(t, db)
	assertColumn(t, db, `user`, `role`, `character varying`, false)
}
