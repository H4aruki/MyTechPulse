package recommendation

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

// forbiddenClient は、呼ばれたら試験を失敗にする提供元。DBから読むときは外部APIを呼ばないことを確かめる。
type forbiddenClient struct{ t *testing.T }

func (f forbiddenClient) Search(context.Context, string) ([]article.Article, error) {
	f.t.Errorf("DBから読むときに外部APIの検索が呼ばれた")
	return nil, errors.New("forbidden")
}
func (f forbiddenClient) Popular(context.Context, time.Time) ([]article.Article, error) {
	f.t.Errorf("DBから読むときに外部APIの人気の記事が呼ばれた")
	return nil, errors.New("forbidden")
}

type taggedCall struct {
	source string
	tags   []string
	since  time.Time
	limit  int
}

type popularCall struct {
	source string
	since  time.Time
	limit  int
}

// memoryArticles は、DBの問い合わせと同じ絞り方・並べ方をする試験用の記事置き場。
type memoryArticles struct {
	items        []article.Article
	err          error
	taggedCalls  []taggedCall
	popularCalls []popularCall
}

func (m *memoryArticles) ListTagged(_ context.Context, source string, tags []string, since time.Time, limit int) ([]article.Article, error) {
	m.taggedCalls = append(m.taggedCalls, taggedCall{source, append([]string(nil), tags...), since, limit})
	if m.err != nil {
		return nil, m.err
	}
	want := map[string]bool{}
	for _, tag := range tags {
		want[interest.NormalizeTag(tag)] = true
	}
	out := []article.Article{}
	for _, item := range m.items {
		if item.Source != source || item.PublishedAt.Before(since) {
			continue
		}
		for _, tag := range item.Tags {
			if want[interest.NormalizeTag(tag)] {
				out = append(out, item)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PublishedAt.After(out[j].PublishedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memoryArticles) ListPopular(_ context.Context, source string, since time.Time, limit int) ([]article.Article, error) {
	m.popularCalls = append(m.popularCalls, popularCall{source, since, limit})
	if m.err != nil {
		return nil, m.err
	}
	out := []article.Article{}
	for _, item := range m.items {
		if item.Source == source && !item.PublishedAt.Before(since) {
			out = append(out, item)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Likes > out[j].Likes })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

var dbNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func dbService(t *testing.T, weights []interest.Weight, repo *memoryArticles) Service {
	return Service{
		Interests:   &memoryInterests{weights: weights},
		Providers:   ProviderSet{Qiita: forbiddenClient{t}, Zenn: forbiddenClient{t}},
		Articles:    repo,
		Clock:       fixedClock{dbNow},
		FeedTimeout: time.Second,
	}
}

func fiveTags() []interest.Weight {
	return []interest.Weight{
		{TagID: 1, Tag: "Go", Value: 5000}, {TagID: 2, Tag: "Rust", Value: 4000}, {TagID: 3, Tag: "AWS", Value: 3000},
		{TagID: 4, Tag: "Docker", Value: 2000}, {TagID: 5, Tag: "React", Value: 1000},
	}
}

func TestServiceGetFromDBDoesNotCallProvidersAndRespectsWindows(t *testing.T) {
	day := 24 * time.Hour
	repo := &memoryArticles{items: []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/edge", []string{"go"}, 1, dbNow.Add(-5*day)),
		testArticle(article.SourceQiita, "https://qiita.com/old", []string{"go"}, 1, dbNow.Add(-5*day-time.Second)),
		testArticle(article.SourceZenn, "https://zenn.dev/fresh", []string{"Rust"}, 1, dbNow.Add(-14*day)),
		testArticle(article.SourceZenn, "https://zenn.dev/old", []string{"Go"}, 1, dbNow.Add(-20*day)),
		testArticle(article.SourceQiita, "https://qiita.com/other", []string{"Kotlin"}, 1, dbNow),
	}}
	got, err := dbService(t, fiveTags(), repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.QiitaArticles); len(u) != 1 || u[0] != "https://qiita.com/edge" {
		t.Fatalf("Qiita = %v", u)
	}
	// 14日以内のZenn記事があるので、古い記事は使わない
	if u := urls(got.ZennArticles); len(u) != 1 || u[0] != "https://zenn.dev/fresh" {
		t.Fatalf("Zenn = %v", u)
	}
	if got.Warnings == nil || len(got.Warnings) != 0 {
		t.Fatalf("Warnings = %#v", got.Warnings)
	}
	// 興味度が0より大きいタグが5つあるので、人気の記事は読まない
	if len(repo.popularCalls) != 0 {
		t.Fatalf("popular calls = %+v", repo.popularCalls)
	}
	if len(repo.taggedCalls) != 2 {
		t.Fatalf("tagged calls = %+v", repo.taggedCalls)
	}
	for _, call := range repo.taggedCalls {
		if len(call.tags) != 5 || call.limit != dbTaggedLimit {
			t.Fatalf("call = %+v", call)
		}
		switch call.source {
		case article.SourceQiita:
			if !call.since.Equal(dbNow.Add(-5 * day)) {
				t.Fatalf("Qiita since = %v", call.since)
			}
		case article.SourceZenn:
			if !call.since.Equal(dbNow.Add(-30 * day)) {
				t.Fatalf("Zenn since = %v", call.since)
			}
		default:
			t.Fatalf("source = %q", call.source)
		}
	}
}

func TestServiceGetFromDBFallsBackToOldZennOnlyWithoutFreshTagged(t *testing.T) {
	old := dbNow.Add(-20 * 24 * time.Hour)
	repo := &memoryArticles{items: []article.Article{
		testArticle(article.SourceZenn, "https://zenn.dev/old", []string{"Go"}, 1, old),
		// 人気の記事は14日以内にあっても、古い記事での埋め合わせを止めない
		testArticle(article.SourceZenn, "https://zenn.dev/popular", []string{"Kotlin"}, 9, dbNow.Add(-time.Hour)),
	}}
	weights := []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.ZennArticles); len(u) != 2 || u[0] != "https://zenn.dev/old" || u[1] != "https://zenn.dev/popular" {
		t.Fatalf("Zenn = %v", u)
	}
}

func TestServiceGetFromDBFillsWithPopularOnlyWhenFewerThanFiveTags(t *testing.T) {
	repo := &memoryArticles{items: []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/tagged", []string{"Go"}, 0, dbNow.Add(-time.Hour)),
		testArticle(article.SourceQiita, "https://qiita.com/popular", []string{"Kotlin"}, 50, dbNow.Add(-time.Hour)),
		testArticle(article.SourceQiita, "https://qiita.com/popular-old", []string{"Kotlin"}, 99, dbNow.Add(-6*24*time.Hour)),
		testArticle(article.SourceZenn, "https://zenn.dev/popular", nil, 3, dbNow.Add(-time.Hour)),
	}}
	weights := []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}, {TagID: 2, Tag: "Rust", Value: 0}}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.QiitaArticles); len(u) != 2 || u[0] != "https://qiita.com/tagged" || u[1] != "https://qiita.com/popular" {
		t.Fatalf("Qiita = %v", u)
	}
	if u := urls(got.ZennArticles); len(u) != 1 || u[0] != "https://zenn.dev/popular" {
		t.Fatalf("Zenn = %v", u)
	}
	// 興味度0のタグは検索に使わない
	for _, call := range repo.taggedCalls {
		if len(call.tags) != 1 || call.tags[0] != "Go" {
			t.Fatalf("tagged call = %+v", call)
		}
	}
	if len(repo.popularCalls) != 2 {
		t.Fatalf("popular calls = %+v", repo.popularCalls)
	}
	for _, call := range repo.popularCalls {
		want := dbNow.Add(-qiitaWindow)
		if call.source == article.SourceZenn {
			want = dbNow.Add(-zennWindow)
		}
		if !call.since.Equal(want) || call.limit != dbPopularLimit {
			t.Fatalf("popular call = %+v", call)
		}
	}

	// 興味度が0より大きいタグが無い利用者は、タグで探さず、人気の記事だけを読む
	repo.taggedCalls, repo.popularCalls = nil, nil
	got, err = dbService(t, nil, repo).Get(context.Background(), 1)
	if err != nil || len(got.QiitaArticles) != 2 || len(got.ZennArticles) != 1 {
		t.Fatalf("got %#v err %v", got, err)
	}
	if len(repo.taggedCalls) != 0 || len(repo.popularCalls) != 2 {
		t.Fatalf("calls: tagged %+v popular %+v", repo.taggedCalls, repo.popularCalls)
	}
}

func TestServiceGetFromDBScoresWithAllArticleTagsAndLimitsToTen(t *testing.T) {
	weights := fiveTags()
	items := []article.Article{
		// 1つ目のタグだけで見つかっても、記事に付いた全タグで点数を付ける
		testArticle(article.SourceQiita, "https://qiita.com/multi", []string{"React", "Go", "Rust"}, 0, dbNow.Add(-time.Hour)),
		testArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 0, dbNow.Add(-time.Hour)),
		testArticle(article.SourceZenn, "https://zenn.dev/aws-likes", []string{"AWS"}, 100, dbNow.Add(-time.Hour)),
		testArticle(article.SourceZenn, "https://zenn.dev/go", []string{"go"}, 0, dbNow.Add(-time.Hour)),
	}
	for i := range 12 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/react/"+string(rune('a'+i)), []string{"React"}, 0, dbNow.Add(-time.Hour)))
	}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 10 {
		t.Fatalf("Qiita = %d件", len(got.QiitaArticles))
	}
	if u := urls(got.QiitaArticles); u[0] != "https://qiita.com/multi" || u[1] != "https://qiita.com/go" {
		t.Fatalf("Qiita = %v", u)
	}
	// Zennはいいね数を掛けない
	if u := urls(got.ZennArticles); len(u) != 2 || u[0] != "https://zenn.dev/go" || u[1] != "https://zenn.dev/aws-likes" {
		t.Fatalf("Zenn = %v", u)
	}
}

func TestServiceGetFromDBSkipsInvalidArticles(t *testing.T) {
	bad := testArticle(article.SourceQiita, "http://qiita.com/insecure", []string{"Go"}, 1, dbNow.Add(-time.Hour))
	repo := &memoryArticles{items: []article.Article{bad, testArticle(article.SourceQiita, "https://qiita.com/ok", []string{"Go"}, 1, dbNow.Add(-time.Hour))}}
	got, err := dbService(t, fiveTags(), repo).Get(context.Background(), 1)
	if err != nil || len(got.Warnings) != 0 {
		t.Fatalf("got %#v err %v", got, err)
	}
	if u := urls(got.QiitaArticles); len(u) != 1 || u[0] != "https://qiita.com/ok" {
		t.Fatalf("Qiita = %v", u)
	}
}

func TestServiceGetFromDBReturnsRepositoryError(t *testing.T) {
	dbErr := errors.New("db down")
	for _, weights := range [][]interest.Weight{fiveTags(), nil} {
		repo := &memoryArticles{err: dbErr}
		if _, err := dbService(t, weights, repo).Get(context.Background(), 1); !errors.Is(err, dbErr) {
			t.Fatalf("err = %v", err)
		}
	}
}

// deadlineArticles は、渡された ctx に期限があるかを記録する。
type deadlineArticles struct {
	memoryArticles
	hadDeadline bool
}

func (d *deadlineArticles) ListTagged(ctx context.Context, source string, tags []string, since time.Time, limit int) ([]article.Article, error) {
	_, d.hadDeadline = ctx.Deadline()
	return d.memoryArticles.ListTagged(ctx, source, tags, since, limit)
}

func TestServiceGetFromDBUsesFeedTimeout(t *testing.T) {
	repo := &deadlineArticles{}
	s := dbService(t, fiveTags(), nil)
	s.Articles = repo
	if _, err := s.Get(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if !repo.hadDeadline {
		t.Fatal("DBの問い合わせに期限が付いていない")
	}
}
