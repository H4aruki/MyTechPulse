package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/store"
)

func feedURLs(items []article.Article) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.URL)
	}
	return out
}

func TestFeedArticlesListTaggedMatchesNormalizedTagsWithinWindow(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	fetch := store.NewArticleFetch(pool)
	feed := store.NewFeedArticles(pool)
	goTag := newTag(t, pool, " Go ")
	rustTag := newTag(t, pool, "Rust")
	kotlinTag := newTag(t, pool, "Kotlin")
	since := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

	newer := record(article.SourceQiita, "q-new", "https://qiita.com/u/items/q-new", since.Add(2*time.Hour), goTag, rustTag)
	newer.Article.Likes = 7
	newer.Article.Author = "alice"
	recs := []articlefetch.Record{
		newer,
		record(article.SourceQiita, "q-edge", "https://qiita.com/u/items/q-edge", since, goTag), // ちょうど境目は含む
		record(article.SourceQiita, "q-old", "https://qiita.com/u/items/q-old", since.Add(-time.Second), goTag),
		record(article.SourceQiita, "q-kotlin", "https://qiita.com/u/items/q-kotlin", since.Add(time.Hour), kotlinTag),
		record(article.SourceQiita, "q-none", "https://qiita.com/u/items/q-none", since.Add(time.Hour)),
		record(article.SourceZenn, "z-go", "https://zenn.dev/u/articles/z-go", since.Add(time.Hour), goTag),
	}
	if res, err := fetch.SaveArticles(ctx, recs); err != nil || res.Saved != len(recs) {
		t.Fatalf("res = %+v, err = %v", res, err)
	}

	// 大文字小文字と前後の空白を無視して比べる。知らないタグは何にも当たらない
	got, err := feed.ListTagged(ctx, article.SourceQiita, []string{"  GO", "unknown"}, since, 100)
	if err != nil {
		t.Fatal(err)
	}
	if u := feedURLs(got); !reflect.DeepEqual(u, []string{"https://qiita.com/u/items/q-new", "https://qiita.com/u/items/q-edge"}) {
		t.Fatalf("urls = %v", u)
	}
	first := got[0]
	// 1つのタグで見つかっても、記事に付いた全タグを返す。提供元は画面の表記に戻す
	if first.Source != article.SourceQiita || first.Title != "title q-new" || first.Likes != 7 || first.Author != "alice" ||
		!first.PublishedAt.Equal(since.Add(2*time.Hour)) || !reflect.DeepEqual(first.Tags, []string{" Go ", "Rust"}) {
		t.Fatalf("article = %+v", first)
	}
	if _, err := article.Validate(first); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if first.ID <= 0 {
		t.Fatalf("ID = %d, DBの記事のIDが入っていない", first.ID)
	}

	// 複数のタグに当たっても、記事は1件だけ返す
	got, err = feed.ListTagged(ctx, article.SourceQiita, []string{"go", "rust", "kotlin"}, since, 100)
	if err != nil {
		t.Fatal(err)
	}
	if u := feedURLs(got); !reflect.DeepEqual(u, []string{"https://qiita.com/u/items/q-new", "https://qiita.com/u/items/q-kotlin", "https://qiita.com/u/items/q-edge"}) {
		t.Fatalf("urls = %v", u)
	}

	// 件数の上限
	got, err = feed.ListTagged(ctx, article.SourceQiita, []string{"go"}, since, 1)
	if err != nil || len(got) != 1 || got[0].URL != "https://qiita.com/u/items/q-new" {
		t.Fatalf("got = %v, err = %v", feedURLs(got), err)
	}

	// 提供元で分ける
	got, err = feed.ListTagged(ctx, article.SourceZenn, []string{"go"}, since, 100)
	if err != nil || len(got) != 1 || got[0].Source != article.SourceZenn || got[0].URL != "https://zenn.dev/u/articles/z-go" {
		t.Fatalf("zenn = %+v, err = %v", got, err)
	}

	// タグが無ければ空
	got, err = feed.ListTagged(ctx, article.SourceQiita, []string{" "}, since, 100)
	if err != nil || len(got) != 0 {
		t.Fatalf("got = %v, err = %v", feedURLs(got), err)
	}
}

func TestFeedArticlesListPopularOrdersByLikesWithinWindow(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	fetch := store.NewArticleFetch(pool)
	feed := store.NewFeedArticles(pool)
	goTag := newTag(t, pool, "Go")
	since := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)

	withLikes := func(rec articlefetch.Record, likes int) articlefetch.Record {
		rec.Article.Likes = likes
		return rec
	}
	recs := []articlefetch.Record{
		withLikes(record(article.SourceZenn, "few", "https://zenn.dev/u/articles/few", since.Add(time.Hour), goTag), 2),
		withLikes(record(article.SourceZenn, "many", "https://zenn.dev/u/articles/many", since.Add(time.Hour)), 50),
		withLikes(record(article.SourceZenn, "mid", "https://zenn.dev/u/articles/mid", since), 10),
		withLikes(record(article.SourceZenn, "old", "https://zenn.dev/u/articles/old", since.Add(-time.Second)), 999),
		withLikes(record(article.SourceQiita, "q", "https://qiita.com/u/items/q", since.Add(time.Hour)), 500),
	}
	if res, err := fetch.SaveArticles(ctx, recs); err != nil || res.Saved != len(recs) {
		t.Fatalf("res = %+v, err = %v", res, err)
	}

	got, err := feed.ListPopular(ctx, article.SourceZenn, since, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://zenn.dev/u/articles/many", "https://zenn.dev/u/articles/mid", "https://zenn.dev/u/articles/few"}
	if u := feedURLs(got); !reflect.DeepEqual(u, want) {
		t.Fatalf("urls = %v", u)
	}
	// タグの無い記事は空の一覧、タグのある記事はそのタグを返す
	if got[0].Tags == nil || len(got[0].Tags) != 0 || !reflect.DeepEqual(got[2].Tags, []string{"Go"}) {
		t.Fatalf("tags = %v / %v", got[0].Tags, got[2].Tags)
	}
	for _, item := range got {
		if item.Source != article.SourceZenn {
			t.Fatalf("source = %q", item.Source)
		}
	}

	got, err = feed.ListPopular(ctx, article.SourceZenn, since, 2)
	if err != nil || !reflect.DeepEqual(feedURLs(got), want[:2]) {
		t.Fatalf("got = %v, err = %v", feedURLs(got), err)
	}
}

func TestFeedArticlesListCooccurringTagsCountsSharedArticles(t *testing.T) {
	ctx := context.Background()
	pool := newTestPool(t)
	fetch := store.NewArticleFetch(pool)
	feed := store.NewFeedArticles(pool)
	goTag := newTag(t, pool, "Go")
	rustTag := newTag(t, pool, "Rust")
	dockerTag := newTag(t, pool, "Docker")
	kotlinTag := newTag(t, pool, "Kotlin")
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

	recs := []articlefetch.Record{
		record(article.SourceQiita, "q1", "https://qiita.com/u/items/q1", now, goTag, rustTag),
		record(article.SourceQiita, "q2", "https://qiita.com/u/items/q2", now.Add(-time.Hour), goTag, rustTag),
		// 公開から100日たっている。問い合わせは日時で絞らないので、表に残っていれば数に入る
		record(article.SourceZenn, "z1", "https://zenn.dev/u/articles/z1", now.Add(-100*24*time.Hour), goTag, rustTag),
		record(article.SourceQiita, "d1", "https://qiita.com/u/items/d1", now, goTag, dockerTag),
		record(article.SourceQiita, "d2", "https://qiita.com/u/items/d2", now, goTag, dockerTag),
		record(article.SourceQiita, "triple", "https://qiita.com/u/items/triple", now, goTag, rustTag, dockerTag),
		record(article.SourceQiita, "k1", "https://qiita.com/u/items/k1", now, goTag, kotlinTag),
		record(article.SourceQiita, "only-go", "https://qiita.com/u/items/only-go", now, goTag),
		record(article.SourceQiita, "only-kotlin", "https://qiita.com/u/items/only-kotlin", now, kotlinTag),
	}
	if res, err := fetch.SaveArticles(ctx, recs); err != nil || res.Saved != len(recs) {
		t.Fatalf("res = %+v, err = %v", res, err)
	}

	// 空白と大文字小文字を無視する。下限はここでは掛けない（回数の小さいタグも返す）
	got, err := feed.ListCooccurringTags(ctx, []string{"  GO "}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Tag != "Rust" || got[0].Count != 4 || got[1].Tag != "Docker" || got[1].Count != 3 || got[2].Tag != "Kotlin" || got[2].Count != 1 {
		t.Fatalf("counts = %+v", got)
	}
	for _, item := range got {
		if interest.NormalizeTag(item.Tag) == "go" {
			t.Fatalf("種のタグ自身が返った: %+v", got)
		}
	}

	// 興味度があるタグは除く。Go と Rust の両方を種にしても、Docker は記事1件につき1回
	got, err = feed.ListCooccurringTags(ctx, []string{"go", "rust"}, []string{" Rust ", "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Tag != "Docker" || got[0].Count != 3 || got[1].Tag != "Kotlin" || got[1].Count != 1 {
		t.Fatalf("excluded = %+v", got)
	}

	got, err = feed.ListCooccurringTags(ctx, []string{" "}, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("blank = %+v, err = %v", got, err)
	}
	got, err = feed.ListCooccurringTags(ctx, []string{"nosuch"}, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("unknown = %+v, err = %v", got, err)
	}
}
