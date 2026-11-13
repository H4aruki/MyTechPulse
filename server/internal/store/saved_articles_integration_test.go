package store_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

// newSavableArticles は記事を n 件作り、そのIDを作成順に返す。
func newSavableArticles(t *testing.T, pool *pgxpool.Pool, prefix string, n int) []int64 {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`INSERT INTO articles (source, external_id, url, title, published_at)
		 SELECT 'qiita', $1 || g, 'https://qiita.com/u/items/' || $1 || g, 'title ' || g, now() FROM generate_series(1, $2::int) g
		 RETURNING id`, prefix, n)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func mustSave(t *testing.T, s *store.SavedArticles, userID, articleID int64) {
	t.Helper()
	if created, err := s.Save(context.Background(), userID, articleID); err != nil || !created {
		t.Fatalf("Save(%d, %d): created = %v, err = %v", userID, articleID, created, err)
	}
}

func savedIDs(items []store.SavedArticle) []int64 {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	return ids
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSavedArticlesSaveIsIdempotentAndKeepsCreatedAt(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "idem", 1)

	mustSave(t, s, user, ids[0])
	var first time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM saved_articles`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if created, err := s.Save(ctx, user, ids[0]); err != nil || created {
		t.Fatalf("2回目: created = %v, err = %v", created, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles`); n != 1 {
		t.Fatalf("saved_articles = %d, want 1", n)
	}
	var second time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at FROM saved_articles`).Scan(&second); err != nil || !second.Equal(first) {
		t.Fatalf("created_at が変わった: %v -> %v (err = %v)", first, second, err)
	}
}

func TestSavedArticlesUnsave(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "un", 2)

	mustSave(t, s, user, ids[0])
	mustSave(t, s, user, ids[1])
	if err := s.Unsave(ctx, user, ids[0]); err != nil {
		t.Fatal(err)
	}
	items, total, err := s.List(ctx, user, 20, 0)
	if err != nil || total != 1 || !equalIDs(savedIDs(items), []int64{ids[1]}) {
		t.Fatalf("items = %v, total = %d, err = %v", savedIDs(items), total, err)
	}
	// 未保存・解除済み・存在しない記事・存在しない利用者の解除も、エラーにならない
	for _, c := range []struct{ user, article int64 }{{user, ids[0]}, {user, 999999}, {999999, ids[1]}, {0, ids[1]}} {
		if err := s.Unsave(ctx, c.user, c.article); err != nil {
			t.Fatalf("Unsave(%d, %d) = %v", c.user, c.article, err)
		}
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles`); n != 1 {
		t.Fatalf("saved_articles = %d, want 1", n)
	}
	// 解除しても記事そのものは残る
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE id = $1`, ids[0]); n != 1 {
		t.Fatalf("記事が消えた")
	}
}

func TestSavedArticlesListOrderAndPaging(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "page", 5)

	for _, id := range ids {
		mustSave(t, s, user, id)
	}
	// 同じ保存日時でも並びが決まる（article_id の降順）
	if _, err := pool.Exec(ctx, `UPDATE saved_articles SET created_at = $1 WHERE article_id IN ($2, $3)`, time.Now().Add(-24*time.Hour), ids[3], ids[4]); err != nil {
		t.Fatal(err)
	}
	want := []int64{ids[2], ids[1], ids[0], ids[4], ids[3]} // 保存の新しい順。同時刻の ids[3], ids[4] は最後で、id の大きい方が先
	// ids[0..2] は now() で保存したので、保存の順に新しい。ids[3], ids[4] は過去に書き換えた
	all, total, err := s.List(ctx, user, 100, 0)
	if err != nil || total != 5 || !equalIDs(savedIDs(all), want) {
		t.Fatalf("all = %v, want %v, total = %d, err = %v", savedIDs(all), want, total, err)
	}
	if all[0].Title == "" || all[0].URL == "" || all[0].Source != "qiita" || all[0].SavedAt.IsZero() || all[0].PublishedAt.IsZero() {
		t.Fatalf("項目が足りない: %+v", all[0])
	}

	page, total, err := s.List(ctx, user, 2, 1)
	if err != nil || total != 5 || !equalIDs(savedIDs(page), want[1:3]) {
		t.Fatalf("page = %v, want %v, total = %d, err = %v", savedIDs(page), want[1:3], total, err)
	}
	// 最後のページは端数。範囲外は空で、総件数はそのまま
	last, total, err := s.List(ctx, user, 2, 4)
	if err != nil || total != 5 || !equalIDs(savedIDs(last), want[4:]) {
		t.Fatalf("last = %v, total = %d, err = %v", savedIDs(last), total, err)
	}
	empty, total, err := s.List(ctx, user, 2, 50)
	if err != nil || total != 5 || len(empty) != 0 {
		t.Fatalf("empty = %v, total = %d, err = %v", savedIDs(empty), total, err)
	}
}

func TestSavedArticlesLimitIs500(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "lim", store.SavedArticlesLimit+1)

	for _, id := range ids[:store.SavedArticlesLimit] {
		mustSave(t, s, user, id)
	}
	if _, err := s.Save(ctx, user, ids[store.SavedArticlesLimit]); !errors.Is(err, store.ErrSavedLimitReached) {
		t.Fatalf("501件目: err = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles`); n != store.SavedArticlesLimit {
		t.Fatalf("saved_articles = %d", n)
	}
	// 保存済みの記事の再保存は、上限でも成功する
	if created, err := s.Save(ctx, user, ids[0]); err != nil || created {
		t.Fatalf("再保存: created = %v, err = %v", created, err)
	}
	// 1件解除すれば、また保存できる。上限は利用者ごと
	if err := s.Unsave(ctx, user, ids[0]); err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, user, ids[store.SavedArticlesLimit])
	other := newPasskeyUser(t, pool, "bob")
	mustSave(t, s, other, ids[0])
}

func TestSavedArticlesLimitHoldsUnderConcurrentSaves(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "conc", store.SavedArticlesLimit+10)
	for _, id := range ids[:store.SavedArticlesLimit-3] {
		mustSave(t, s, user, id)
	}

	var wg sync.WaitGroup
	results := make([]error, 10)
	for i, id := range ids[store.SavedArticlesLimit-3 : store.SavedArticlesLimit+7] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = s.Save(ctx, user, id)
		}()
	}
	wg.Wait()
	limited := 0
	for _, err := range results {
		switch {
		case err == nil:
		case errors.Is(err, store.ErrSavedLimitReached):
			limited++
		default:
			t.Fatalf("err = %v", err)
		}
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles`); n != store.SavedArticlesLimit || limited != 7 {
		t.Fatalf("saved_articles = %d (want %d), limited = %d (want 7)", n, store.SavedArticlesLimit, limited)
	}
}

func TestSavedArticlesUnknownArticleOrUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "nf", 1)

	if _, err := s.Save(ctx, user, 999999); !errors.Is(err, store.ErrArticleNotFound) {
		t.Fatalf("存在しない記事: err = %v", err)
	}
	if _, err := s.Save(ctx, 999999, ids[0]); !errors.Is(err, store.ErrSavedUserNotFound) {
		t.Fatalf("存在しない利用者: err = %v", err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles`); n != 0 {
		t.Fatalf("saved_articles = %d", n)
	}
}

func TestSavedArticlesListShowsOnlyOwnSaves(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")
	ids := newSavableArticles(t, pool, "own", 3)

	mustSave(t, s, alice, ids[0])
	mustSave(t, s, bob, ids[1])
	mustSave(t, s, bob, ids[2])
	items, total, err := s.List(ctx, alice, 20, 0)
	if err != nil || total != 1 || !equalIDs(savedIDs(items), []int64{ids[0]}) {
		t.Fatalf("alice: %v, total = %d, err = %v", savedIDs(items), total, err)
	}
	items, total, err = s.List(ctx, bob, 20, 0)
	if err != nil || total != 2 || !equalIDs(savedIDs(items), []int64{ids[2], ids[1]}) {
		t.Fatalf("bob: %v, total = %d, err = %v", savedIDs(items), total, err)
	}
	// 保存のない利用者・存在しない利用者は空
	if items, total, err := s.List(ctx, 999999, 20, 0); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("存在しない利用者: %v, total = %d, err = %v", items, total, err)
	}
}

func TestSavedArticlesAreDeletedWithTheUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")
	ids := newSavableArticles(t, pool, "del", 2)
	mustSave(t, s, alice, ids[0])
	mustSave(t, s, bob, ids[0])

	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE "user_ID" = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE "user_ID" = $1`, alice); n != 0 {
		t.Fatalf("退会した利用者の保存が残っている: %d", n)
	}
	if items, total, err := s.List(ctx, bob, 20, 0); err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("bob の保存が消えた: %v, total = %d, err = %v", items, total, err)
	}
}

func TestArticleRetentionKeepsSavedArticles(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	fetch := store.NewArticleFetch(pool)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	cutoff := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)

	recs := []articlefetch.Record{
		record(article.SourceQiita, "keep-saved", "https://qiita.com/u/items/keep-saved", cutoff.Add(-time.Hour)),
		record(article.SourceQiita, "drop-unsaved", "https://qiita.com/u/items/drop-unsaved", cutoff.Add(-time.Hour)),
		record(article.SourceQiita, "recent", "https://qiita.com/u/items/recent", cutoff.Add(time.Hour)),
	}
	if res, err := fetch.SaveArticles(ctx, recs); err != nil || res.Saved != 3 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	var savedID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM articles WHERE external_id = 'keep-saved'`).Scan(&savedID); err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, user, savedID)

	n, err := fetch.DeleteExpired(ctx, "qiita", cutoff)
	if err != nil || n != 1 {
		t.Fatalf("deleted = %d, err = %v", n, err)
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'keep-saved'`); got != 1 {
		t.Fatalf("保存された古い記事が消えた")
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'drop-unsaved'`); got != 0 {
		t.Fatalf("保存されていない古い記事が残った")
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'recent'`); got != 1 {
		t.Fatalf("新しい記事が消えた")
	}

	// 解除すると、次の削除で消える
	if err := s.Unsave(ctx, user, savedID); err != nil {
		t.Fatal(err)
	}
	if n, err := fetch.DeleteExpired(ctx, "qiita", cutoff); err != nil || n != 1 {
		t.Fatalf("解除後: deleted = %d, err = %v", n, err)
	}
}

// 保存の途中（saved_articles の挿入が未確定）の記事を、保持期間の削除が消さないこと（Issue #294）。
// 外部キーの検査で、未確定の保存は記事の行に共有ロックを付けている。
// 旧実装（1文の DELETE）は、開始時のスナップショットで「保存なし」と判断した記事のロックを待ち、
// 保存の確定後にそのまま消して、保存の行まで連動して消していた。この試験では、削除が確定を待ってタイムアウトし、失敗する。
// 今の実装は、ロックされた記事を飛ばして即座に0件で返り、保存の確定後も記事と保存の行が残る。
func TestArticleRetentionSkipsArticleBeingSaved(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	fetch := store.NewArticleFetch(pool)
	user := newPasskeyUser(t, pool, "alice")
	cutoff := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)

	recs := []articlefetch.Record{
		record(article.SourceQiita, "saving", "https://qiita.com/u/items/saving", cutoff.Add(-time.Hour)),
	}
	if res, err := fetch.SaveArticles(ctx, recs); err != nil || res.Saved != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	var articleID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM articles WHERE external_id = 'saving'`).Scan(&articleID); err != nil {
		t.Fatal(err)
	}

	// 保存のトランザクションを、確定しないまま開いておく
	saving, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = saving.Rollback(ctx) }()
	if _, err := saving.Exec(ctx, `INSERT INTO saved_articles ("user_ID", article_id) VALUES ($1, $2)`, user, articleID); err != nil {
		t.Fatal(err)
	}

	// 別の接続で削除を走らせる。待たずに0件で返る（待つ実装ならここでタイムアウトする）
	deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if n, err := fetch.DeleteExpired(deleteCtx, "qiita", cutoff); err != nil || n != 0 {
		t.Fatalf("保存の途中の削除: deleted = %d, err = %v", n, err)
	}

	if err := saving.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE id = $1`, articleID); got != 1 {
		t.Fatalf("保存の途中の記事が消えた")
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE "user_ID" = $1 AND article_id = $2`, user, articleID); got != 1 {
		t.Fatalf("保存の行が消えた")
	}

	// 確定したあとの削除でも、保存済みなので消さない
	if n, err := fetch.DeleteExpired(ctx, "qiita", cutoff); err != nil || n != 0 {
		t.Fatalf("確定後の削除: deleted = %d, err = %v", n, err)
	}
}

// tagArticle は、記事にタグを付ける（tag 表に無ければ作る）。
func tagArticle(t *testing.T, pool *pgxpool.Pool, articleID int64, tags ...string) {
	t.Helper()
	ensureTags(t, pool, tags...)
	lowered := make([]string, len(tags))
	for i, tag := range tags {
		lowered[i] = strings.ToLower(strings.TrimSpace(tag))
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO article_tags (article_id, "tag_ID", published_at)
		 SELECT $1, "tag_ID", now() FROM tag WHERE lower(btrim(tag_name)) = ANY($2::text[])`, articleID, lowered); err != nil {
		t.Fatal(err)
	}
}

// weightsOf は、利用者の興味度を、小文字のタグ名 → match_int で返す。
func weightsOf(t *testing.T, pool *pgxpool.Pool, userID int64) map[string]int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT lower(btrim(t.tag_name)), r.match_int FROM recommend r JOIN tag t ON t."tag_ID" = r."tag_ID" WHERE r."user_ID" = $1`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var name string
		var v int
		if err := rows.Scan(&name, &v); err != nil {
			t.Fatal(err)
		}
		out[name] = v
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func setWeight(t *testing.T, pool *pgxpool.Pool, userID int64, tag string, v int) {
	t.Helper()
	ensureTags(t, pool, tag)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO recommend ("user_ID", "tag_ID", match_int) SELECT $1, "tag_ID", $3 FROM tag WHERE lower(btrim(tag_name)) = lower($2)
		 ON CONFLICT ("user_ID", "tag_ID") DO UPDATE SET match_int = EXCLUDED.match_int`, userID, tag, v); err != nil {
		t.Fatal(err)
	}
}

func assertWeights(t *testing.T, pool *pgxpool.Pool, userID int64, want map[string]int) {
	t.Helper()
	if got := weightsOf(t, pool, userID); !reflect.DeepEqual(got, want) {
		t.Fatalf("興味度 = %v, want %v", got, want)
	}
}

// backdateSaves は、利用者の保存を、反映の期限（SaveLearnDelay）を過ぎた時刻にずらす（10秒待たないため）。
func backdateSaves(t *testing.T, pool *pgxpool.Pool, userID int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE saved_articles SET created_at = now() - interval '11 seconds' WHERE "user_ID" = $1`, userID); err != nil {
		t.Fatal(err)
	}
}

func mustList(t *testing.T, in *store.Interest, userID int64) {
	t.Helper()
	if _, err := in.List(context.Background(), userID); err != nil {
		t.Fatalf("Interest.List: %v", err)
	}
}

func TestSavedArticlesLearnsOnlyAfterTheDelay(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "learn", 1)
	tagArticle(t, pool, ids[0], "Go", "Rust")
	setWeight(t, pool, user, "go", 5000)
	setWeight(t, pool, user, "python", 7000) // 記事に無いタグ
	tagsBefore := queryInt(t, pool, `SELECT count(*) FROM tag`)
	before := map[string]int{"go": 5000, "python": 7000}

	// (a) 保存した直後は、保存も一覧も、点を変えない
	mustSave(t, s, user, ids[0])
	assertWeights(t, pool, user, before)
	mustList(t, in, user)
	assertWeights(t, pool, user, before)

	// (e) 期限前の再PUT（200）は、行も未反映の数も増やさず、点も変えない
	if created, err := s.Save(ctx, user, ids[0]); err != nil || created {
		t.Fatalf("再保存: created = %v, err = %v", created, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE learned_at IS NULL`); n != 1 {
		t.Fatalf("未反映の保存 = %d, want 1", n)
	}
	assertWeights(t, pool, user, before)

	// (c) 期限を過ぎて読むと、既存のタグは +3000、行の無かったタグは 3000 で行ができる。記事に無いタグは減衰しない
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	after := map[string]int{"go": 8000, "rust": 3000, "python": 7000}
	assertWeights(t, pool, user, after)
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != tagsBefore {
		t.Fatalf("tag 表の行が増えた: %d -> %d", tagsBefore, n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE learned_at IS NOT NULL`); n != 1 {
		t.Fatalf("反映済みの保存 = %d, want 1", n)
	}

	// (c) 何度読んでも、再PUTしても、解除しても、二重に足さない
	mustList(t, in, user)
	if created, err := s.Save(ctx, user, ids[0]); err != nil || created {
		t.Fatalf("再保存: created = %v, err = %v", created, err)
	}
	mustList(t, in, user)
	assertWeights(t, pool, user, after)
	if err := s.Unsave(ctx, user, ids[0]); err != nil {
		t.Fatal(err)
	}
	mustList(t, in, user)
	assertWeights(t, pool, user, after)
}

func TestSavedArticlesUnsaveWithinTheDelayDoesNotLearn(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "undo", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, user, "go", 5000)

	// (b) 期限内に解除すれば、行が無いので、時刻が過ぎても反映されない
	mustSave(t, s, user, ids[0])
	if err := s.Unsave(ctx, user, ids[0]); err != nil {
		t.Fatal(err)
	}
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 5000})
	// クリックは知らないタグでも全タグを ×0.8 にする。保存の +3000 が入っていれば 6400 になる
	if _, err := in.UpdateForClick(ctx, user, []string{"zzz-no-such-tag"}); err != nil {
		t.Fatal(err)
	}
	assertWeights(t, pool, user, map[string]int{"go": 4000})
}

func TestSavedArticlesLearningSurvivesRestart(t *testing.T) {
	pool := newTestPool(t)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "restart", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, user, "go", 5000)

	// (d) 保存した側のインスタンスを捨てて作り直しても、DBの記録から反映される。1回だけ
	mustSave(t, store.NewSavedArticles(pool), user, ids[0])
	backdateSaves(t, pool, user)
	mustList(t, store.NewInterest(pool), user)
	assertWeights(t, pool, user, map[string]int{"go": 8000})
	mustList(t, store.NewInterest(pool), user)
	assertWeights(t, pool, user, map[string]int{"go": 8000})
}

func TestSavedArticlesLearnsForSeveralArticlesAndAfterClick(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "multi", 2)
	tagArticle(t, pool, ids[0], "Go")
	tagArticle(t, pool, ids[1], "Go", "Rust")
	setWeight(t, pool, user, "go", 5000)
	setWeight(t, pool, user, "python", 7000)

	// 複数の記事は、まとめて反映する（Go は2回分で上限の10000、Rust は新しい行）
	mustSave(t, s, user, ids[0])
	mustSave(t, s, user, ids[1])
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 10000, "rust": 3000, "python": 7000})

	// (f) クリック済みの記事を保存しても +3000。ほかのタグは減衰しない
	ids = newSavableArticles(t, pool, "clk", 1)
	tagArticle(t, pool, ids[0], "Java")
	setWeight(t, pool, user, "java", 5000)
	if _, err := in.UpdateForClick(ctx, user, []string{"Java"}); err != nil { // 5000×0.8+2000 = 6000。ほかは ×0.8
		t.Fatal(err)
	}
	assertWeights(t, pool, user, map[string]int{"go": 8000, "rust": 2400, "python": 5600, "java": 6000})
	mustSave(t, s, user, ids[0])
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 8000, "rust": 2400, "python": 5600, "java": 9000})
}

func TestSavedArticlesClickAppliesDueSaveFirstAndSkipsPendingOne(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "order", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, user, "go", 5000)
	setWeight(t, pool, user, "python", 5000)

	// (g) 期限前のクリックは、保存の分をまだ入れない: go は 5000×0.8 = 4000、python は 4000
	mustSave(t, s, user, ids[0])
	if _, err := in.UpdateForClick(ctx, user, []string{"zzz-no-such-tag"}); err != nil {
		t.Fatal(err)
	}
	assertWeights(t, pool, user, map[string]int{"go": 4000, "python": 4000})
	// そのあと期限が来たら入る
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 7000, "python": 4000})

	// 期限後の保存は、次のクリックが減衰より先に反映する: 7000 + 3000 = 10000 → ×0.8 = 8000
	ids = newSavableArticles(t, pool, "order2", 1)
	tagArticle(t, pool, ids[0], "Go")
	mustSave(t, s, user, ids[0])
	backdateSaves(t, pool, user)
	if _, err := in.UpdateForClick(ctx, user, []string{"zzz-no-such-tag"}); err != nil {
		t.Fatal(err)
	}
	assertWeights(t, pool, user, map[string]int{"go": 8000, "python": 3200})
}

func TestSavedArticlesConcurrentReadsLearnOnce(t *testing.T) {
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "race", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, user, "go", 5000)
	mustSave(t, s, user, ids[0])
	backdateSaves(t, pool, user)

	// (h) 同時に読んでも、1回だけ足す
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = store.NewInterest(pool).List(context.Background(), user)
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertWeights(t, pool, user, map[string]int{"go": 8000})
}

func TestSavedArticlesFailedSaveDoesNotLearn(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "fail", store.SavedArticlesLimit+1)
	tagArticle(t, pool, ids[store.SavedArticlesLimit], "Go")
	setWeight(t, pool, user, "go", 5000)
	for _, id := range ids[:store.SavedArticlesLimit] {
		mustSave(t, s, user, id) // タグの無い記事。学習しない
	}

	// (f) 上限の409と、知らない記事の404
	if _, err := s.Save(ctx, user, ids[store.SavedArticlesLimit]); !errors.Is(err, store.ErrSavedLimitReached) {
		t.Fatalf("501件目: err = %v", err)
	}
	if _, err := s.Save(ctx, user, 999999); !errors.Is(err, store.ErrArticleNotFound) {
		t.Fatalf("存在しない記事: err = %v", err)
	}
	backdateSaves(t, pool, user)
	mustList(t, store.NewInterest(pool), user)
	assertWeights(t, pool, user, map[string]int{"go": 5000})
}

func TestSavedArticlesSaveDoesNotChangeOtherUsersInterest(t *testing.T) {
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")
	ids := newSavableArticles(t, pool, "other", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, bob, "go", 4000)

	// (f) alice の保存の反映は、bob を変えない
	mustSave(t, s, alice, ids[0])
	backdateSaves(t, pool, alice)
	mustList(t, in, alice)
	mustList(t, in, bob)
	assertWeights(t, pool, alice, map[string]int{"go": 3000})
	assertWeights(t, pool, bob, map[string]int{"go": 4000})
}

// resave は、保存を解除してもう一度保存し、反映の期限を過ぎた状態にする。
func resave(t *testing.T, pool *pgxpool.Pool, s *store.SavedArticles, userID, articleID int64) {
	t.Helper()
	if err := s.Unsave(context.Background(), userID, articleID); err != nil {
		t.Fatal(err)
	}
	mustSave(t, s, userID, articleID)
	backdateSaves(t, pool, userID)
}

func TestSavedArticlesLearnOncePerUserAndArticle(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "once", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, user, "go", 5000)

	// (a) 最初の保存は +3000
	mustSave(t, s, user, ids[0])
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 8000})

	// 解除して再保存し、期限を過ぎて読んでも、足さない。saved_articles の印は付く
	resave(t, pool, s, user, ids[0])
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 8000})
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_articles WHERE learned_at IS NULL`); n != 0 {
		t.Fatalf("未反映の保存 = %d, want 0", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_article_learnings`); n != 1 {
		t.Fatalf("saved_article_learnings = %d, want 1", n)
	}

	// クリックで読んでも足さない（全タグ ×0.8 だけ）。8000 × 0.8 = 6400
	resave(t, pool, s, user, ids[0])
	if _, err := in.UpdateForClick(ctx, user, []string{"zzz-no-such-tag"}); err != nil {
		t.Fatal(err)
	}
	assertWeights(t, pool, user, map[string]int{"go": 6400})
}

func TestSavedArticlesLearnOnceDoesNotBlockOtherArticles(t *testing.T) {
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "other-art", 2)
	tagArticle(t, pool, ids[0], "Go")
	tagArticle(t, pool, ids[1], "Rust")
	setWeight(t, pool, user, "go", 5000)

	// (b) 同じ利用者の別の記事は、学習する
	mustSave(t, s, user, ids[0])
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	mustSave(t, s, user, ids[1])
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 8000, "rust": 3000})
}

func TestSavedArticlesLearnOnceIsPerUser(t *testing.T) {
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")
	ids := newSavableArticles(t, pool, "per-user", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, alice, "go", 5000)
	setWeight(t, pool, bob, "go", 4000)

	// (c) alice が学習済みでも、別の利用者の bob は、同じ記事で学習する
	mustSave(t, s, alice, ids[0])
	backdateSaves(t, pool, alice)
	mustList(t, in, alice)
	mustSave(t, s, bob, ids[0])
	backdateSaves(t, pool, bob)
	mustList(t, in, bob)
	assertWeights(t, pool, alice, map[string]int{"go": 8000})
	assertWeights(t, pool, bob, map[string]int{"go": 7000})
}

func TestSavedArticleLearningsAreDeletedWithTheUser(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	alice, bob := newPasskeyUser(t, pool, "alice"), newPasskeyUser(t, pool, "bob")
	ids := newSavableArticles(t, pool, "learn-del", 1)
	tagArticle(t, pool, ids[0], "Go")
	for _, u := range []int64{alice, bob} {
		mustSave(t, s, u, ids[0])
		backdateSaves(t, pool, u)
		mustList(t, in, u)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_article_learnings`); n != 2 {
		t.Fatalf("saved_article_learnings = %d, want 2", n)
	}

	// (d) 退会すると、その利用者の記録も消える。ほかの利用者の記録は残る
	if _, err := pool.Exec(ctx, `DELETE FROM "user" WHERE "user_ID" = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_article_learnings WHERE "user_ID" = $1`, alice); n != 0 {
		t.Fatalf("退会した利用者の記録が残っている: %d", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM saved_article_learnings WHERE "user_ID" = $1`, bob); n != 1 {
		t.Fatalf("bob の記録 = %d, want 1", n)
	}
}

func TestSavedArticlesUnsaveBeforeLearningThenResaveLearnsOnce(t *testing.T) {
	pool := newTestPool(t)
	s := store.NewSavedArticles(pool)
	in := store.NewInterest(pool)
	user := newPasskeyUser(t, pool, "alice")
	ids := newSavableArticles(t, pool, "early-undo", 1)
	tagArticle(t, pool, ids[0], "Go")
	setWeight(t, pool, user, "go", 5000)

	// (e) 学習する前（期限内）に解除したので、まだ一度も学習していない。あとで再保存して期限を過ぎたら、1回だけ学習する
	mustSave(t, s, user, ids[0])
	if err := s.Unsave(context.Background(), user, ids[0]); err != nil {
		t.Fatal(err)
	}
	backdateSaves(t, pool, user)
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 5000})
	resave(t, pool, s, user, ids[0])
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 8000})
	mustList(t, in, user)
	assertWeights(t, pool, user, map[string]int{"go": 8000})
}
