package store_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/provider/qiita"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

func newTag(t *testing.T, pool *pgxpool.Pool, name string) int32 {
	t.Helper()
	var id int32
	if err := pool.QueryRow(context.Background(), `INSERT INTO tag(tag_name) VALUES ($1) RETURNING "tag_ID"`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func record(source, externalID, url string, published time.Time, tagIDs ...int32) articlefetch.Record {
	return articlefetch.Record{
		Article: article.Article{
			Source: source, ExternalID: externalID, URL: url, Title: "title " + externalID,
			Author: "author", Likes: 1, PublishedAt: published,
		},
		TagIDs: tagIDs,
	}
}

var articlePublished = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

func TestArticleFetchUpsertIsIdempotentAndKeepsFetchedAt(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	goTag := newTag(t, pool, "Go")

	first := record(article.SourceQiita, "a1", "https://qiita.com/u/items/a1", articlePublished, goTag)
	if res, err := s.SaveArticles(ctx, []articlefetch.Record{first}); err != nil || res.Saved != 1 || res.Skipped != 0 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	var fetchedAt, seenAt time.Time
	if err := pool.QueryRow(ctx, `SELECT fetched_at, last_seen_at FROM articles WHERE external_id = 'a1'`).Scan(&fetchedAt, &seenAt); err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	second := first
	second.Article.Likes = 9
	second.Article.Title = "changed"
	if res, err := s.SaveArticles(ctx, []articlefetch.Record{second}); err != nil || res.Saved != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles`); n != 1 {
		t.Fatalf("articles = %d, want 1", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM article_tags`); n != 1 {
		t.Fatalf("article_tags = %d, want 1", n)
	}
	var likes int
	var title, source string
	var fetchedAfter, seenAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT likes, title, source, fetched_at, last_seen_at FROM articles WHERE external_id = 'a1'`).Scan(&likes, &title, &source, &fetchedAfter, &seenAfter); err != nil {
		t.Fatal(err)
	}
	if likes != 9 || title != "changed" || source != "qiita" {
		t.Fatalf("likes = %d, title = %q, source = %q", likes, title, source)
	}
	if !fetchedAfter.Equal(fetchedAt) {
		t.Fatalf("fetched_at が変わった: %v -> %v", fetchedAt, fetchedAfter)
	}
	if !seenAfter.After(seenAt) {
		t.Fatalf("last_seen_at が更新されない: %v -> %v", seenAt, seenAfter)
	}
	// 記事のタグは重複せず、公開日時が写されている
	var tagPublished time.Time
	if err := pool.QueryRow(ctx, `SELECT published_at FROM article_tags`).Scan(&tagPublished); err != nil || !tagPublished.Equal(articlePublished) {
		t.Fatalf("article_tags.published_at = %v, err = %v", tagPublished, err)
	}
}

func TestArticleFetchTagsOnlyLinkToExistingTagsAndNeverCreateTags(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	goTag := newTag(t, pool, "Go")
	tagsBefore := queryInt(t, pool, `SELECT count(*) FROM tag`)

	recs := []articlefetch.Record{
		record(article.SourceQiita, "ok1", "https://qiita.com/u/items/ok1", articlePublished, goTag),
		record(article.SourceQiita, "bad", "https://qiita.com/u/items/bad", articlePublished, goTag, 999999), // 存在しないタグ
		record(article.SourceQiita, "ok2", "https://qiita.com/u/items/ok2", articlePublished),                // タグ0件でも記事は保存
	}
	res, err := s.SaveArticles(ctx, recs)
	if err != nil {
		t.Fatal(err)
	}
	if res.Saved != 2 || res.Skipped != 1 {
		t.Fatalf("res = %+v", res)
	}
	// 存在しないタグを含む記事は、その記事だけ巻き戻る(記事もタグ付けも残らない)
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id = 'bad'`); n != 0 {
		t.Fatalf("bad が残っている")
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM article_tags`); n != 1 {
		t.Fatalf("article_tags = %d, want 1", n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != tagsBefore {
		t.Fatalf("tag が増えた: %d -> %d", tagsBefore, n)
	}
}

func TestArticleFetchSkipsOnlyTheArticleWithDuplicateURL(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	recs := []articlefetch.Record{
		record(article.SourceZenn, "a", "https://zenn.dev/u/articles/same", articlePublished),
		record(article.SourceZenn, "b", "https://zenn.dev/u/articles/same", articlePublished), // 別のslugで同じURL
		record(article.SourceZenn, "c", "https://zenn.dev/u/articles/other", articlePublished),
	}
	res, err := s.SaveArticles(ctx, recs)
	if err != nil || res.Saved != 2 || res.Skipped != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE external_id IN ('a', 'c')`); n != 2 {
		t.Fatalf("a と c が保存されていない")
	}
}

func TestArticleFetchRetentionBoundaries(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	goTag := newTag(t, pool, "Go")
	cutoff := time.Date(2026, 10, 17, 3, 0, 0, 0, time.UTC)

	recs := []articlefetch.Record{
		record(article.SourceQiita, "q-old", "https://qiita.com/u/items/q-old", cutoff.Add(-time.Second), goTag),
		record(article.SourceQiita, "q-edge", "https://qiita.com/u/items/q-edge", cutoff, goTag), // ちょうど境目は残す
		record(article.SourceQiita, "q-new", "https://qiita.com/u/items/q-new", cutoff.Add(time.Second), goTag),
		record(article.SourceZenn, "z-old", "https://zenn.dev/u/articles/z-old", cutoff.Add(-time.Second), goTag),
	}
	if res, err := s.SaveArticles(ctx, recs); err != nil || res.Saved != 4 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	n, err := s.DeleteExpired(ctx, "qiita", cutoff)
	if err != nil || n != 1 {
		t.Fatalf("deleted = %d, err = %v", n, err)
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE source = 'qiita'`); got != 2 {
		t.Fatalf("qiita articles = %d", got)
	}
	// Zennは、Qiitaの期限では消えない
	if got := queryInt(t, pool, `SELECT count(*) FROM articles WHERE source = 'zenn'`); got != 1 {
		t.Fatalf("zenn articles = %d", got)
	}
	// 消えた記事のタグ付けも一緒に消える
	if got := queryInt(t, pool, `SELECT count(*) FROM article_tags`); got != 3 {
		t.Fatalf("article_tags = %d, want 3", got)
	}
	if n, err := s.DeleteExpired(ctx, "zenn", cutoff); err != nil || n != 1 {
		t.Fatalf("zenn deleted = %d, err = %v", n, err)
	}
	if got := queryInt(t, pool, `SELECT count(*) FROM article_tags`); got != 2 {
		t.Fatalf("article_tags = %d, want 2", got)
	}
}

func TestArticleFetchListFetchTagsReturnsEnabledOnlyUpTo500(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	on := newTag(t, pool, "Spring Boot")
	off := newTag(t, pool, "Disabled")
	if _, err := pool.Exec(ctx, `INSERT INTO fetch_tags("tag_ID", zenn_topic, enabled) VALUES ($1, 'springboot', true), ($2, 'disabled', false)`, on, off); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListFetchTags(ctx)
	if err != nil || len(got) != 1 || got[0].ID != on || got[0].ZennTopic != "springboot" || got[0].Name != "Spring Boot" {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO tag(tag_name) SELECT 'bulk' || g FROM generate_series(1, 510) g`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO fetch_tags("tag_ID", zenn_topic) SELECT "tag_ID", lower(tag_name) FROM tag WHERE tag_name LIKE 'bulk%'`); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListFetchTags(ctx)
	if err != nil || len(got) != 500 {
		t.Fatalf("len = %d, err = %v", len(got), err)
	}
	tags, err := s.ListTags(ctx)
	if err != nil || len(tags) != 512 {
		t.Fatalf("tags = %d, err = %v", len(tags), err)
	}
}

func TestArticleFetchRunHistory(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	slot := time.Date(2026, 10, 22, 6, 0, 0, 0, time.FixedZone("JST", 9*3600))
	started := time.Date(2026, 10, 21, 21, 0, 5, 0, time.UTC)

	id1, err := s.StartRun(ctx, slot, articlefetch.TriggerScheduled, 1, started)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.RunsForSlot(ctx, slot)
	if err != nil || len(rows) != 1 || rows[0].ID != id1 || rows[0].Status != articlefetch.StatusRunning || rows[0].Attempt != 1 || !rows[0].FinishedAt.IsZero() {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}

	finished := started.Add(2 * time.Minute)
	res := articlefetch.Result{Status: articlefetch.StatusFailed, Message: "qiita: provider rate limited (status 429)", QiitaRequests: 12, ZennRequests: 3, Upserted: 40, Deleted: 5}
	if err := s.FinishRun(ctx, id1, finished, res); err != nil {
		t.Fatal(err)
	}
	var status, msg, trigger string
	var q, z, up, del int
	var gotFinished time.Time
	if err := pool.QueryRow(ctx, `SELECT status, error_message, trigger, qiita_requests, zenn_requests, upserted, deleted, finished_at FROM fetch_runs WHERE id = $1`, id1).
		Scan(&status, &msg, &trigger, &q, &z, &up, &del, &gotFinished); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || msg != res.Message || trigger != "scheduled" || q != 12 || z != 3 || up != 40 || del != 5 || !gotFinished.Equal(finished) {
		t.Fatalf("row = %s %q %s %d %d %d %d %v", status, msg, trigger, q, z, up, del, gotFinished)
	}

	// 2回目(再実行)。履歴は新しい順
	id2, err := s.StartRun(ctx, slot, articlefetch.TriggerRetry, 2, started.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.StartRun(ctx, slot.Add(5*time.Hour+30*time.Minute), articlefetch.TriggerScheduled, 1, started.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	rows, err = s.RunsForSlot(ctx, slot)
	if err != nil || len(rows) != 2 || rows[0].ID != id2 || rows[1].ID != id1 || rows[1].Status != articlefetch.StatusFailed || !rows[1].FinishedAt.Equal(finished) {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}

	// 実行中のまま残った行は、失敗として閉じられる(終わっている行は変えない)
	abandonAt := started.Add(time.Hour)
	if err := s.AbandonStaleRuns(ctx, abandonAt); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{id2, other} {
		var st, m string
		var fin time.Time
		if err := pool.QueryRow(ctx, `SELECT status, error_message, finished_at FROM fetch_runs WHERE id = $1`, id).Scan(&st, &m, &fin); err != nil {
			t.Fatal(err)
		}
		if st != "failed" || m == "" || !fin.Equal(abandonAt) {
			t.Fatalf("run %d = %s %q %v", id, st, m, fin)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM fetch_runs WHERE id = $1`, id1).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("status = %s, err = %v", status, err)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM fetch_runs WHERE finished_at = $1`, finished); n != 1 {
		t.Fatalf("終わっている行が書き換えられた")
	}
}

func TestArticleFetchRunRejectsUnknownStatus(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	id, err := s.StartRun(ctx, time.Now(), articlefetch.TriggerCatchup, 1, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, id, time.Now(), articlefetch.Result{Status: "weird"}); err == nil {
		t.Fatal("不正な状態を受け付けた")
	}
}

func TestArticleFetchAdvisoryLockIsExclusive(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	a, b := store.NewArticleFetch(pool), store.NewArticleFetch(pool) // 別のAPIプロセスを想定

	unlockA, ok, err := a.TryLock(ctx)
	if err != nil || !ok {
		t.Fatalf("1つ目: ok = %v, err = %v", ok, err)
	}
	if _, ok, err := b.TryLock(ctx); err != nil || ok {
		t.Fatalf("2つ目が取れた: ok = %v, err = %v", ok, err)
	}
	// 同じプロセスの2回目も取れない(別の接続なので)
	if _, ok, err := a.TryLock(ctx); err != nil || ok {
		t.Fatalf("同じ側の2回目が取れた: ok = %v, err = %v", ok, err)
	}
	// ロックは専用の接続で持つので、プールの接続を借りたままにしない
	if stat := pool.Stat(); stat.AcquiredConns() != 0 {
		t.Fatalf("プールの接続を占有している: %d", stat.AcquiredConns())
	}
	unlockA()
	unlockB, ok, err := b.TryLock(ctx)
	if err != nil || !ok {
		t.Fatalf("解放後に取れない: ok = %v, err = %v", ok, err)
	}
	unlockB()
	if n := queryInt(t, pool, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`); n != 0 {
		t.Fatalf("ロックが残っている: %d", n)
	}
}

// 取得の一連の流れ(偽の提供元 → 実際のDB)。tag 表の行は作られず、記事のタグは既存のタグにだけ結ばれる。
func TestJobWithRealStoreNeverCreatesTags(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	goTag := newTag(t, pool, "Go")
	newTag(t, pool, "Spring Boot")
	if _, err := pool.Exec(ctx, `INSERT INTO fetch_tags("tag_ID", zenn_topic) VALUES ($1, 'go')`, goTag); err != nil {
		t.Fatal(err)
	}
	tagsBefore := queryInt(t, pool, `SELECT count(*) FROM tag`)

	published := time.Now().Add(-time.Hour)
	q := &stubQiita{fn: func(day time.Time, page int) qiita.Page {
		a := article.Article{
			Source: article.SourceQiita, ExternalID: "q-" + day.Format("0102"), Author: "alice", Title: "t",
			URL: "https://qiita.com/alice/items/q-" + day.Format("0102"), Likes: 2, PublishedAt: published,
			Tags: []string{"go", "SpringBoot", "ThisTagDoesNotExist"},
		}
		return qiita.Page{Articles: []article.Article{a}, RateRemaining: -1}
	}}
	z := &stubZenn{fn: func(topic string) []article.Article {
		return []article.Article{{
			Source: article.SourceZenn, ExternalID: topic + "-slug", Author: "bob", Title: "z",
			URL: "https://zenn.dev/bob/articles/" + topic + "-slug", Likes: 1, PublishedAt: published,
		}}
	}}
	job := &articlefetch.Job{Store: s, Qiita: q, Zenn: z, Clock: articlefetch.SystemClock{}, QiitaMaxRequests: 300}
	res := job.Execute(ctx)
	if res.Status != articlefetch.StatusSucceeded {
		t.Fatalf("res = %+v", res)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM tag`); n != tagsBefore {
		t.Fatalf("tag が増えた: %d -> %d", tagsBefore, n)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles WHERE source = 'qiita'`); n < 5 {
		t.Fatalf("qiita articles = %d", n)
	}
	// Qiitaの記事: go と SpringBoot(=Spring Boot) の2本。知らないタグは結ばれない
	if n := queryInt(t, pool, `SELECT count(*) FROM article_tags at JOIN articles a ON a.id = at.article_id WHERE a.source = 'qiita'`); n != 2*queryInt(t, pool, `SELECT count(*) FROM articles WHERE source = 'qiita'`) {
		t.Fatalf("qiita の article_tags = %d", n)
	}
	// Zennの記事: 検索に使った取得対象のタグ(Go)だけ
	if n := queryInt(t, pool, `SELECT count(*) FROM article_tags at JOIN articles a ON a.id = at.article_id WHERE a.source = 'zenn' AND at."tag_ID" = $1`, goTag); n != 1 {
		t.Fatalf("zenn の article_tags = %d", n)
	}
	if res.Upserted < 6 || res.QiitaRequests < 6 || res.ZennRequests != 1 {
		t.Fatalf("res = %+v", res)
	}
	// 2回流しても記事は増えない
	count := queryInt(t, pool, `SELECT count(*) FROM articles`)
	if res := job.Execute(ctx); res.Status != articlefetch.StatusSucceeded {
		t.Fatalf("res = %+v", res)
	}
	if n := queryInt(t, pool, `SELECT count(*) FROM articles`); n != count {
		t.Fatalf("2回目で記事が増えた: %d -> %d", count, n)
	}
}

// 実際のDBの履歴・ロックとスケジューラの組み合わせ: 起動時の追いつきが1回だけ記録される。
func TestSchedulerWithRealStoreRecordsCatchUpOnce(t *testing.T) {
	pool := newTestPool(t)
	s := store.NewArticleFetch(pool)
	exec := &countingExec{}
	newSched := func() *articlefetch.Scheduler {
		return &articlefetch.Scheduler{Runs: s, Locker: s, Job: exec, Clock: articlefetch.SystemClock{}, RunTimeout: time.Minute}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ { // 2つのAPIが同時に起動した場合
		sched := newSched()
		go func() { sched.Run(ctx); done <- struct{}{} }()
	}
	deadline := time.Now().Add(10 * time.Second)
	for queryInt(t, pool, `SELECT count(*) FROM fetch_runs WHERE status = 'succeeded'`) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // もう1つの側が重ねて実行しないことを確かめる
	cancel()
	<-done
	<-done
	if n := queryInt(t, pool, `SELECT count(*) FROM fetch_runs`); n != 1 {
		t.Fatalf("fetch_runs = %d, want 1", n)
	}
	var trigger, status string
	if err := pool.QueryRow(context.Background(), `SELECT trigger, status FROM fetch_runs`).Scan(&trigger, &status); err != nil || trigger != "catchup" || status != "succeeded" {
		t.Fatalf("trigger = %s, status = %s, err = %v", trigger, status, err)
	}
	if exec.n() != 1 {
		t.Fatalf("実行回数 = %d", exec.n())
	}
}

type countingExec struct{ calls atomic.Int32 }

func (c *countingExec) Execute(context.Context) articlefetch.Result {
	c.calls.Add(1)
	return articlefetch.Result{Status: articlefetch.StatusSucceeded}
}

func (c *countingExec) n() int { return int(c.calls.Load()) }

type stubQiita struct {
	fn func(day time.Time, page int) qiita.Page
}

func (s *stubQiita) ListCreated(_ context.Context, day time.Time, page int) (qiita.Page, error) {
	return s.fn(day, page), nil
}

func (s *stubQiita) FullPage(qiita.Page) bool { return false }

type stubZenn struct {
	fn func(topic string) []article.Article
}

func (s *stubZenn) SearchTopic(_ context.Context, topic string) ([]article.Article, error) {
	return s.fn(topic), nil
}
