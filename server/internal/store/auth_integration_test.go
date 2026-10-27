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
	if err == nil {
		// 移行(00006)が入れる会員登録用のタグを消し、各試験が空のtag表から始められるようにする
		_, err = db.Exec(`DELETE FROM tag`)
	}
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

// ensureTags は、tag 表に無いタグ(前後の空白と大文字小文字を無視)だけを入れる。
// 会員登録(#262)はタグを作らず、tag 表にあるタグだけを選べるため、試験は登録の前に使うタグを用意する。
func ensureTags(t *testing.T, pool *pgxpool.Pool, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, err := pool.Exec(context.Background(), `INSERT INTO tag(tag_name) SELECT $1::text
			WHERE NOT EXISTS (SELECT 1 FROM tag WHERE lower(btrim(tag_name)) = lower(btrim($1::text)))`, name); err != nil {
			t.Fatal(err)
		}
	}
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
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('Go'), ('GO'), ('Rust')`); err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{" go ", "Rust"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
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
	// 選んだタグは10000(1.0)
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend r JOIN tag t USING ("tag_ID")
		WHERE r."user_ID" = $1 AND r.match_int = 10000 AND t.tag_name IN ('Go', 'Rust')`, user.ID); n != 2 {
		t.Fatalf("selected recommendations = %d", n)
	}
	// 選んだタグと同じ名前の重複タグ(GO)は、選ばなかったタグとして扱わない
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

func TestAuthRepositorySignupRecordsTermsAgreement(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('Go')`); err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateWithInterestsAndSession(ctx, "agreer", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	// 登録と同時に、同意した版と日時の行ができる
	if n := queryInt(t, pool, `SELECT count(*) FROM user_terms_agreement
		WHERE "user_ID" = $1 AND terms_version = $2 AND agreed_at > now() - interval '1 minute'`, user.ID, auth.CurrentTermsVersion); n != 1 {
		t.Fatalf("agreement rows = %d", n)
	}
	// 失敗した登録(同名)では、同意の行も残らない
	if _, err := repo.CreateWithInterestsAndSession(ctx, "agreer", "hash2", auth.RoleMember,
		[]string{"Go"}, hashOf(2), fixedNow.Add(time.Hour), auth.CurrentTermsVersion); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("err = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM user_terms_agreement`); n != 1 {
		t.Fatalf("agreement rows after failed signup = %d", n)
	}
}

func TestAuthRepositorySignupCreatesRowsOnlyForSelectedTags(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	// 既存の全タグ。選ばなかったタグ(Python, Java)には行を作らない(#240)。
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) VALUES ('Go'), ('Rust'), ('Python'), ('PYTHON'), ('Java')`); err != nil {
		t.Fatal(err)
	}
	user, err := repo.CreateWithInterestsAndSession(ctx, "bob", "hash", auth.RoleMember,
		[]string{"go", "Rust"}, hashOf(2), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"Go": 10000, "Rust": 10000} {
		got := queryInt(t, pool, `SELECT r.match_int FROM recommend r JOIN tag t USING ("tag_ID")
			WHERE r."user_ID" = $1 AND t.tag_name = $2`, user.ID, name)
		if got != want {
			t.Errorf("%s match_int = %d, want %d", name, got, want)
		}
	}
	// Go, Rust の2件だけ。Python・PYTHON・Java には行が無い。
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend WHERE "user_ID" = $1`, user.ID); n != 2 {
		t.Fatalf("recommend rows = %d", n)
	}
	// フィードが使う興味度も、選んだタグだけ
	got, err := store.NewInterest(pool).List(ctx, user.ID)
	if err != nil || len(got) != 2 {
		t.Fatalf("List = %#v, %v", got, err)
	}
}

func TestAuthRepositoryDuplicateUsernameRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	ensureTags(t, pool, "Go", "Other-Tag")
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion); err != nil {
		t.Fatal(err)
	}
	_, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash2", auth.RoleMember,
		[]string{"Other-Tag"}, hashOf(2), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("want ErrUsernameTaken, got %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM "user"`); n != 1 {
		t.Fatalf("user count = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 2 {
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
	ensureTags(t, pool, "Go", "Other-Tag")
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion); err != nil {
		t.Fatal(err)
	}
	// セッションの主キー(同じトークンハッシュ)が衝突し、利用者・初期興味度の作成の後で失敗させる
	_, err := repo.CreateWithInterestsAndSession(ctx, "bob", "hash", auth.RoleMember,
		[]string{"Other-Tag"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err == nil || errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("want generic failure, got %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM "user" WHERE user_name = 'bob'`); n != 0 {
		t.Fatalf("bob must be rolled back, got %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 2 {
		t.Fatalf("tag count = %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend`); n != 1 {
		t.Fatalf("bob's recommend rows must be rolled back, got %d total", n)
	}
}

// #262: 知らないタグが1つでもあれば、tag 表にも利用者・セッション・興味度にも何も作らない。
func TestAuthRepositorySignupWithUnknownTagCreatesNothing(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	ensureTags(t, pool, "Go")
	for _, tags := range [][]string{{"Go", "No-Such-Tag"}, {"No-Such-Tag"}} {
		_, err := repo.CreateWithInterestsAndSession(ctx, "carol", "hash", auth.RoleMember,
			tags, hashOf(7), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
		if !errors.Is(err, auth.ErrUnknownFavoriteTag) {
			t.Fatalf("tags %v: want ErrUnknownFavoriteTag, got %v", tags, err)
		}
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM tag`:          1,
		`SELECT count(*) FROM "user"`:       0,
		`SELECT count(*) FROM auth_session`: 0,
		`SELECT count(*) FROM recommend`:    0,
	} {
		if n := queryInt(t, pool, query); n != want {
			t.Errorf("%s = %d, want %d", query, n, want)
		}
	}
	// 用意したタグだけなら登録できる(大文字小文字・前後の空白は無視)
	if _, err := repo.CreateWithInterestsAndSession(ctx, "carol", "hash", auth.RoleMember,
		[]string{" GO "}, hashOf(7), fixedNow.Add(time.Hour), auth.CurrentTermsVersion); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 1 {
		t.Fatalf("tag count = %d", n)
	}
}

// #262: 既存タグの綴り違い(大文字小文字)で同時に登録しても、全部成功し、タグは増えず、同じタグを指す。
func TestAuthRepositoryConcurrentSignupWithDifferentSpellingsUsesOneTag(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	ensureTags(t, pool, "NewTag")
	var wg sync.WaitGroup
	errs := make([]error, 6)
	spellings := []string{"NewTag", "newtag", "NEWTAG", "newTag", "Newtag", "nEwTaG"}
	for i, spelling := range spellings {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = repo.CreateWithInterestsAndSession(ctx, fmt.Sprintf("user%d", i), "hash", auth.RoleMember,
				[]string{spelling}, hashOf(byte(10+i)), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("signup %d: %v", i, err)
		}
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != 1 {
		t.Fatalf("tag count must stay 1, got %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(DISTINCT "tag_ID") FROM recommend`); n != 1 {
		t.Fatalf("all recommend rows must point to one tag, got %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM recommend`); n != len(spellings) {
		t.Fatalf("recommend rows = %d, want %d", n, len(spellings))
	}
}

// #262: 利用者名が重複していて、かつ知らないタグも含むときは、知らないタグの失敗を返し、何も増やさない。
func TestAuthRepositoryExistingUsernameWithUnknownTagCreatesNothing(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	ensureTags(t, pool, "Go")
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion); err != nil {
		t.Fatal(err)
	}
	_, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash2", auth.RoleMember,
		[]string{"No-Such-Tag"}, hashOf(2), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if !errors.Is(err, auth.ErrUnknownFavoriteTag) {
		t.Fatalf("want ErrUnknownFavoriteTag, got %v", err)
	}
	for query, want := range map[string]int{
		`SELECT count(*) FROM "user"`:       1,
		`SELECT count(*) FROM tag`:          1,
		`SELECT count(*) FROM auth_session`: 1,
		`SELECT count(*) FROM recommend`:    1,
	} {
		if n := queryInt(t, pool, query); n != want {
			t.Errorf("%s = %d, want %d", query, n, want)
		}
	}
}

func TestAuthRepositorySessions(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	ensureTags(t, pool, "Go")
	user, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
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
	ensureTags(t, pool, "Go")
	if _, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember,
		[]string{"Go"}, hash, fixedNow.Add(time.Hour), auth.CurrentTermsVersion); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session WHERE token_hash = convert_to($1, 'UTF8')`, plain); n != 0 {
		t.Fatal("plain token must not be stored")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM auth_session WHERE token_hash = $1`, hash[:]); n != 1 {
		t.Fatal("hash must be stored")
	}
}

func TestAuthRepositoryFindByID(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	repo := store.NewAuth(pool)
	ensureTags(t, pool, "Go")
	created, err := repo.CreateWithInterestsAndSession(ctx, "alice", "hash", auth.RoleMember, []string{"go"}, hashOf(1), fixedNow.Add(time.Hour), auth.CurrentTermsVersion)
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindByID(ctx, created.ID)
	if err != nil || got != created || got.Username != "alice" || got.Role != auth.RoleMember {
		t.Fatalf("FindByID = %+v, %v (created %+v)", got, err, created)
	}
	// 無い・範囲外のIDは ErrNotFound(DB障害と区別する)
	for _, id := range []int64{created.ID + 1000, 0, -1, 1 << 40} {
		if _, err := repo.FindByID(ctx, id); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("FindByID(%d) must be ErrNotFound: %v", id, err)
		}
	}
}
