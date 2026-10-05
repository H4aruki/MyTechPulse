package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/H4aruki/MyTechPulse/server/db/migrations"
	"github.com/H4aruki/MyTechPulse/server/internal/auth"
	"github.com/H4aruki/MyTechPulse/server/internal/migrate"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

// newTestPool はテスト専用の一時DBを作ってマイグレーションを適用し、接続プールを返す。
// 既存DBには触れず、このテストが作ったDBだけを後で破棄する。
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL未設定のためスキップ")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("mtp_storetest_%d", time.Now().UnixNano())
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
	err = migrate.Run(context.Background(), db, migrations.FS)
	db.Close()
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`)
		admin.Close()
	})
	return pool
}

func queryInt(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

func hashOf(b byte) [32]byte {
	var h [32]byte
	for i := range h {
		h[i] = b
	}
	return h
}

var fixedNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestAuthRepositorySignupCreatesUserInterestsAndSession(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	// 大文字小文字だけ異なる既存タグ。最小IDのタグを使う。
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('Go'), ('GO')`); err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{" go ", "Rust"}, hashOf(1), fixedNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == 0 || user.Username != "alice" || user.Role != auth.RoleMember {
		t.Fatalf("user = %+v", user)
	}
	// タグは既存の最小IDを再利用し、新しいタグだけ作る(Go, GO, Rust の3件)
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 3 {
		t.Fatalf("tag count = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend r JOIN tag t USING ("tag_ID")
		WHERE r."user_ID" = $1 AND r.match_int = 1 AND t.tag_name IN ('Go', 'Rust')`, user.ID); n != 2 {
		t.Fatalf("initial recommendations = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID" = $1`, user.ID); n != 2 {
		t.Fatalf("recommend rows = %d", n)
	}
	// signupと同時に作ったセッションで利用者を引ける
	got, err := repo.FindUser(ctx, hashOf(1), fixedNow)
	if err != nil || got.User != user {
		t.Fatalf("FindUser = %+v, %v", got, err)
	}
	found, err := repo.FindByUsername(ctx, "alice")
	if err != nil || found.PasswordHash != "hash" || found.User != user {
		t.Fatalf("FindByUsername = %+v, %v", found, err)
	}
	if _, err := repo.FindByUsername(ctx, "nobody"); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("missing user must be ErrNotFound: %v", err)
	}
}

func TestAuthRepositoryDuplicateUsernameRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	_, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash2", auth.RoleMember,
		[]string{"Brand-New-Tag"}, hashOf(2), fixedNow.Add(time.Hour))
	if !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("want ErrUsernameTaken, got %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM "user"`); n != 1 {
		t.Fatalf("user count = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 1 {
		t.Fatalf("tag count = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session`); n != 1 {
		t.Fatalf("session count = %d", n)
	}
}

func TestAuthRepositoryFailureAfterUserInsertRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// セッションの主キー(同じトークンハッシュ)が衝突し、利用者・タグ作成の後で失敗させる
	_, err := repo.CreateWithInterestsAndSession(ctx, "bob", "hash", auth.RoleMember,
		[]string{"Brand-New-Tag"}, hashOf(1), fixedNow.Add(time.Hour))
	if err == nil || errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("want generic failure, got %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM "user" WHERE user_name = 'bob'`); n != 0 {
		t.Fatalf("bob must be rolled back, got %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 1 {
		t.Fatalf("new tag must be rolled back, got %d", n)
	}
}

func TestAuthRepositoryConcurrentSignupDoesNotDuplicateTagSpelling(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	var wg sync.WaitGroup
	errs := make([]error, 6)
	spellings := []string{"NewTag", "newtag", "NEWTAG", "newTag", "Newtag", "nEwTaG"}
	for i, spelling := range spellings {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = repo.CreateWithInterestsAndSession(ctx, fmt.Sprintf("user%d", i), "hash", auth.RoleMember,
				[]string{spelling}, hashOf(byte(10+i)), fixedNow.Add(time.Hour))
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("signup %d: %v", i, err)
		}
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 1 {
		t.Fatalf("tag spellings must collapse to one row, got %d", n)
	}
}

func TestAuthRepositorySessions(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	user, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	expires := fixedNow.Add(2 * time.Hour)
	if err := repo.Create(ctx, hashOf(2), user.ID, expires); err != nil {
		t.Fatal(err)
	}
	// 期限前は取得でき、期限ちょうど以降は取得できない
	s, err := repo.FindUser(ctx, hashOf(2), expires.Add(-time.Second))
	if err != nil || s.User != user || !s.ExpiresAt.Equal(expires) {
		t.Fatalf("FindUser before expiry = %+v, %v", s, err)
	}
	for _, at := range []time.Time{expires, expires.Add(time.Second)} {
		if _, err := repo.FindUser(ctx, hashOf(2), at); !errors.Is(err, auth.ErrNotFound) {
			t.Fatalf("expired session at %v must be ErrNotFound: %v", at, err)
		}
	}
	if _, err := repo.FindUser(ctx, hashOf(99), fixedNow); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("unknown token must be ErrNotFound: %v", err)
	}
	// 削除は現在のセッションだけを消し、何度でも成功する
	if err := repo.Delete(ctx, hashOf(2)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, hashOf(2)); err != nil {
		t.Fatalf("second delete must succeed: %v", err)
	}
	if _, err := repo.FindUser(ctx, hashOf(2), fixedNow); !errors.Is(err, auth.ErrNotFound) {
		t.Fatalf("deleted session must be ErrNotFound: %v", err)
	}
	if _, err := repo.FindUser(ctx, hashOf(1), fixedNow); err != nil {
		t.Fatalf("other session must survive: %v", err)
	}
	// 期限切れ清掃
	if err := repo.Create(ctx, hashOf(3), user.ID, fixedNow.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteExpired(ctx, fixedNow); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session`); n != 1 {
		t.Fatalf("only the live session must remain, got %d", n)
	}
}

func TestAuthRepositoryStoresOnlyTokenHash(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	plain, hash, err := (auth.RandomTokenGenerator{}).New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hash, fixedNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session WHERE token_hash = convert_to($1, 'UTF8')`, plain); n != 0 {
		t.Fatal("plain token must not be stored")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session WHERE token_hash = $1`, hash[:]); n != 1 {
		t.Fatal("hash must be stored")
	}
}
