package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/articlefetch"
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
