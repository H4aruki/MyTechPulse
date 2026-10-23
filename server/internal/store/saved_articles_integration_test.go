package store_test

import (
	"context"
	"errors"
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
	for i, id := range ids[store.SavedArticlesLimit-3:] {
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
