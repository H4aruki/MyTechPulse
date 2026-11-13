package recommendation

import (
	"context"
	"errors"
	"slices"
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

type cooccurCall struct {
	seeds   []string
	exclude []string
}

// memoryArticles は、DBの問い合わせと同じ絞り方・並べ方をする試験用の記事置き場。
type memoryArticles struct {
	items        []article.Article
	err          error
	popularErr   error
	cooccurErr   error
	taggedCalls  []taggedCall
	popularCalls []popularCall
	cooccurCalls []cooccurCall
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
	if m.popularErr != nil {
		return nil, m.popularErr
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

// ListCooccurringTags は、SQLと同じく、種のタグが付いた記事に一緒に付いている別のタグを数える。
// 表示期間では絞らない。exclude に入ったタグは返さない。記事1件につき、タグは1回だけ数える。
func (m *memoryArticles) ListCooccurringTags(_ context.Context, seedTags, excludeTags []string) ([]TagCooccurrence, error) {
	m.cooccurCalls = append(m.cooccurCalls, cooccurCall{append([]string(nil), seedTags...), append([]string(nil), excludeTags...)})
	if m.err != nil {
		return nil, m.err
	}
	if m.cooccurErr != nil {
		return nil, m.cooccurErr
	}
	seeds := map[string]bool{}
	for _, tag := range seedTags {
		if key := interest.NormalizeTag(tag); key != "" {
			seeds[key] = true
		}
	}
	excluded := map[string]bool{}
	for _, tag := range excludeTags {
		if key := interest.NormalizeTag(tag); key != "" {
			excluded[key] = true
		}
	}
	type acc struct {
		display string
		n       int
	}
	counts := map[string]*acc{}
	for _, item := range m.items {
		seen := map[string]string{}
		for _, tag := range item.Tags {
			key := interest.NormalizeTag(tag)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; !ok {
				seen[key] = tag
			}
		}
		onArticle := make([]string, 0, len(seen))
		for key := range seen {
			if seeds[key] {
				onArticle = append(onArticle, key)
			}
		}
		if len(onArticle) == 0 {
			continue
		}
		for key, display := range seen {
			counted := false
			for _, seed := range onArticle {
				if seed != key {
					counted = true
					break
				}
			}
			if !counted || excluded[key] {
				continue
			}
			if counts[key] == nil {
				counts[key] = &acc{display: display}
			}
			counts[key].n++
		}
	}
	out := make([]TagCooccurrence, 0, len(counts))
	for _, count := range counts {
		out = append(out, TagCooccurrence{Tag: count.display, Count: count.n})
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
	// 5日を過ぎた記事は、人気の記事としても出さない
	if u := urls(got.QiitaArticles); len(u) != 2 || u[0] != "https://qiita.com/edge" || u[1] != "https://qiita.com/other" {
		t.Fatalf("Qiita = %v", u)
	}
	// 14日以内のZenn記事があるので、古い記事は使わない
	if u := urls(got.ZennArticles); len(u) != 1 || u[0] != "https://zenn.dev/fresh" {
		t.Fatalf("Zenn = %v", u)
	}
	if got.Warnings == nil || len(got.Warnings) != 0 {
		t.Fatalf("Warnings = %#v", got.Warnings)
	}
	// タグが5つあっても、10件に満たない提供元は人気の記事で埋める。似たタグは使わない
	if len(repo.cooccurCalls) != 0 {
		t.Fatalf("cooccur calls = %+v", repo.cooccurCalls)
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

func TestServiceGetFromDBFillsRemainingSlotsWithPopularForFiveTagUser(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	items := []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/rust", []string{"Rust"}, 0, hour),
		// タグで見つかり、人気の記事としても返る。重ねて出さない
		testArticle(article.SourceQiita, "https://qiita.com/react-hot", []string{"React"}, 1000, hour),
		// 5日を過ぎた人気の記事は使わない
		testArticle(article.SourceQiita, "https://qiita.com/popular-old", []string{"Kotlin"}, 9999, dbNow.Add(-6*24*time.Hour)),
	}
	for i := range 9 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/popular/"+string(rune('a'+i)), []string{"Kotlin"}, 100-i, hour))
	}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, fiveTags(), repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		// タグで見つけた記事を点数の順に先に並べる(Go 0.5×1、Rust 0.4×1、React 0.1×1001)
		"https://qiita.com/react-hot", "https://qiita.com/go", "https://qiita.com/rust",
		// 空いた7枠を、いいね数の多い人気の記事で埋める
		"https://qiita.com/popular/a", "https://qiita.com/popular/b", "https://qiita.com/popular/c", "https://qiita.com/popular/d",
		"https://qiita.com/popular/e", "https://qiita.com/popular/f", "https://qiita.com/popular/g",
	}
	if u := urls(got.QiitaArticles); !slices.Equal(u, want) {
		t.Fatalf("Qiita = %v", u)
	}
}

func TestServiceGetFromDBSkipsPopularWhenTaggedFillsTen(t *testing.T) {
	items := []article.Article{
		testArticle(article.SourceZenn, "https://zenn.dev/go", []string{"Go"}, 0, dbNow.Add(-time.Hour)),
		testArticle(article.SourceZenn, "https://zenn.dev/popular", []string{"Kotlin"}, 50, dbNow.Add(-time.Hour)),
	}
	for i := range 10 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/go/"+string(rune('a'+i)), []string{"Go"}, 0, dbNow.Add(-time.Hour)))
	}
	items = append(items, testArticle(article.SourceQiita, "https://qiita.com/popular", []string{"Kotlin"}, 999, dbNow.Add(-time.Hour)))
	repo := &memoryArticles{items: items}
	got, err := dbService(t, fiveTags(), repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 10 {
		t.Fatalf("Qiita = %v", urls(got.QiitaArticles))
	}
	for _, u := range urls(got.QiitaArticles) {
		if u == "https://qiita.com/popular" {
			t.Fatalf("Qiita = %v", urls(got.QiitaArticles))
		}
	}
	if u := urls(got.ZennArticles); len(u) != 2 || u[0] != "https://zenn.dev/go" || u[1] != "https://zenn.dev/popular" {
		t.Fatalf("Zenn = %v", u)
	}
	// 10件そろったQiitaでは人気の記事を読まない
	if len(repo.popularCalls) != 1 || repo.popularCalls[0].source != article.SourceZenn {
		t.Fatalf("popular calls = %+v", repo.popularCalls)
	}
}

func TestServiceGetFromDBFillsZennAfterOldFallback(t *testing.T) {
	day := 24 * time.Hour
	repo := &memoryArticles{items: []article.Article{
		testArticle(article.SourceZenn, "https://zenn.dev/old-go", []string{"Go"}, 0, dbNow.Add(-20*day)),
		testArticle(article.SourceZenn, "https://zenn.dev/popular-fresh", []string{"Kotlin"}, 5, dbNow.Add(-14*day)),
		// 14日を過ぎた人気の記事は使わない
		testArticle(article.SourceZenn, "https://zenn.dev/popular-old", []string{"Kotlin"}, 99, dbNow.Add(-14*day-time.Second)),
	}}
	got, err := dbService(t, fiveTags(), repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.ZennArticles); len(u) != 2 || u[0] != "https://zenn.dev/old-go" || u[1] != "https://zenn.dev/popular-fresh" {
		t.Fatalf("Zenn = %v", u)
	}

	// 14日以内にタグで見つけた記事があれば、古い記事は使わず、人気の記事で埋める
	repo.items = append(repo.items, testArticle(article.SourceZenn, "https://zenn.dev/fresh-go", []string{"Go"}, 0, dbNow.Add(-day)))
	got, err = dbService(t, fiveTags(), repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.ZennArticles); len(u) != 2 || u[0] != "https://zenn.dev/fresh-go" || u[1] != "https://zenn.dev/popular-fresh" {
		t.Fatalf("Zenn = %v", u)
	}
}

func TestServiceGetFromDBWithoutPositiveTagsUsesPopularOnly(t *testing.T) {
	items := []article.Article{}
	for i := range 12 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/popular/"+string(rune('a'+i)), []string{"Kotlin"}, 100-i, dbNow.Add(-time.Hour)))
	}
	repo := &memoryArticles{items: items}
	weights := []interest.Weight{{TagID: 1, Tag: "Go", Value: 0}}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.QiitaArticles); len(u) != 10 || u[0] != "https://qiita.com/popular/a" || u[9] != "https://qiita.com/popular/j" {
		t.Fatalf("Qiita = %v", u)
	}
	// 興味度が0より大きいタグが無い利用者は、タグで探さない
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
	// タグの記事は読めても、人気の記事が読めなければエラーを返す
	repo := &memoryArticles{popularErr: dbErr}
	if _, err := dbService(t, fiveTags(), repo).Get(context.Background(), 1); !errors.Is(err, dbErr) {
		t.Fatalf("popular err = %v", err)
	}
	if len(repo.taggedCalls) != 2 {
		t.Fatalf("tagged calls = %+v", repo.taggedCalls)
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

func TestServiceGetFromDBFillsWithSimilarTagsBeforePopular(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	old := dbNow.Add(-10 * 24 * time.Hour)
	repo := &memoryArticles{items: []article.Article{
		// 5日より古いのでフィードには出ない。共起の数には入る
		testArticle(article.SourceQiita, "https://qiita.com/old/a", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/b", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/c", []string{"Go", "Rust"}, 1, old),
		// 共起2回は下限未満。記事は人気の枠に回る
		testArticle(article.SourceQiita, "https://qiita.com/old/d", []string{"Go", "Docker"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/e", []string{"Go", "Docker"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/both", []string{"Go", "Rust"}, 2, hour),
		testArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 1, hour),
		testArticle(article.SourceQiita, "https://qiita.com/rust-high", []string{"Rust"}, 8, hour),
		testArticle(article.SourceQiita, "https://qiita.com/rust-low", []string{"Rust"}, 3, hour),
		testArticle(article.SourceQiita, "https://qiita.com/docker", []string{"Docker"}, 50, hour),
		testArticle(article.SourceQiita, "https://qiita.com/kotlin", []string{"Kotlin"}, 100, hour),
	}}
	got, err := dbService(t, []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 興味のあるタグ、似たタグ（いいね順）、人気の記事（いいね順）。点数0どうしでも、似たタグを人気より前に置く
	want := []string{
		"https://qiita.com/both", "https://qiita.com/go",
		"https://qiita.com/rust-high", "https://qiita.com/rust-low",
		"https://qiita.com/kotlin", "https://qiita.com/docker",
	}
	if u := urls(got.QiitaArticles); !slices.Equal(u, want) {
		t.Fatalf("Qiita = %v", u)
	}
	if len(repo.cooccurCalls) != 1 {
		t.Fatalf("cooccur calls = %+v", repo.cooccurCalls)
	}
	if !slices.Equal(repo.cooccurCalls[0].seeds, []string{"go"}) || !slices.Equal(repo.cooccurCalls[0].exclude, []string{"go"}) {
		t.Fatalf("cooccur = %+v", repo.cooccurCalls[0])
	}
}

func TestServiceGetFromDBKeepsSimilarInsideTheFreshWindow(t *testing.T) {
	day := 24 * time.Hour
	repo := &memoryArticles{items: []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/old/a", []string{"Go", "Rust"}, 1, dbNow.Add(-10*day)),
		testArticle(article.SourceQiita, "https://qiita.com/old/b", []string{"Go", "Rust"}, 1, dbNow.Add(-10*day)),
		testArticle(article.SourceQiita, "https://qiita.com/old/c", []string{"Go", "Rust"}, 1, dbNow.Add(-10*day)),
		testArticle(article.SourceZenn, "https://zenn.dev/old-go", []string{"Go"}, 0, dbNow.Add(-20*day)),
		testArticle(article.SourceZenn, "https://zenn.dev/old-rust", []string{"Rust"}, 99, dbNow.Add(-20*day)),
		testArticle(article.SourceZenn, "https://zenn.dev/fresh-rust", []string{"Rust"}, 1, dbNow.Add(-time.Hour)),
		testArticle(article.SourceZenn, "https://zenn.dev/kotlin", []string{"Kotlin"}, 100, dbNow.Add(-time.Hour)),
	}}
	got, err := dbService(t, []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 14日を過ぎた似たタグの記事は出さない。古い興味タグの記事のうしろに、14日以内の似たタグ、そのうしろに人気
	want := []string{"https://zenn.dev/old-go", "https://zenn.dev/fresh-rust", "https://zenn.dev/kotlin"}
	if u := urls(got.ZennArticles); !slices.Equal(u, want) {
		t.Fatalf("Zenn = %v", u)
	}
	var saw bool
	for _, call := range repo.taggedCalls {
		if call.source != article.SourceZenn || !slices.Contains(call.tags, "Rust") {
			continue
		}
		saw = true
		if !call.since.Equal(dbNow.Add(-zennWindow)) {
			t.Fatalf("similar since = %v", call.since)
		}
	}
	if !saw {
		t.Fatal("Zennの似たタグを読んでいない")
	}
}

func TestServiceGetFromDBSkipsSimilarForAFullSource(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	old := dbNow.Add(-10 * 24 * time.Hour)
	items := []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/old/a", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/b", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/c", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceZenn, "https://zenn.dev/rust", []string{"Rust"}, 1, hour),
		testArticle(article.SourceZenn, "https://zenn.dev/kotlin", []string{"Kotlin"}, 100, hour),
	}
	for i := range 10 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/go/"+string(rune('a'+i)), []string{"Go"}, 0, hour))
	}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 10 {
		t.Fatalf("Qiita = %v", urls(got.QiitaArticles))
	}
	if u := urls(got.ZennArticles); !slices.Equal(u, []string{"https://zenn.dev/rust", "https://zenn.dev/kotlin"}) {
		t.Fatalf("Zenn = %v", u)
	}
	if len(repo.cooccurCalls) != 1 {
		t.Fatalf("cooccur calls = %d", len(repo.cooccurCalls))
	}
	for _, call := range repo.taggedCalls {
		if call.source == article.SourceQiita && slices.Contains(call.tags, "Rust") {
			t.Fatalf("10件そろったQiitaで似たタグを読んだ: %+v", call)
		}
	}
	if len(repo.popularCalls) != 1 || repo.popularCalls[0].source != article.SourceZenn {
		t.Fatalf("popular calls = %+v", repo.popularCalls)
	}
}

func TestServiceGetFromDBSkipsCooccurrenceWhenBothSourcesAreFull(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	items := []article.Article{}
	for i := range 10 {
		items = append(items,
			testArticle(article.SourceQiita, "https://qiita.com/go/"+string(rune('a'+i)), []string{"Go"}, 0, hour),
			testArticle(article.SourceZenn, "https://zenn.dev/go/"+string(rune('a'+i)), []string{"Go"}, 0, hour),
		)
	}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 10 || len(got.ZennArticles) != 10 {
		t.Fatalf("Qiita %d Zenn %d", len(got.QiitaArticles), len(got.ZennArticles))
	}
	if len(repo.cooccurCalls) != 0 || len(repo.popularCalls) != 0 {
		t.Fatalf("cooccur %+v popular %+v", repo.cooccurCalls, repo.popularCalls)
	}
}

func TestServiceGetFromDBLimitsSimilarTagsToEmptySlots(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	old := dbNow.Add(-10 * 24 * time.Hour)
	items := []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/rust", []string{"Rust"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/aws", []string{"AWS"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/docker", []string{"Docker"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/kotlin", []string{"Kotlin"}, 1, hour),
		testArticle(article.SourceQiita, "https://qiita.com/swift", []string{"Swift"}, 100, hour),
		testArticle(article.SourceQiita, "https://qiita.com/java", []string{"Java"}, 50, hour),
	}
	for i := range 5 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/old/k/"+string(rune('a'+i)), []string{"Go", "Kotlin"}, 1, old))
	}
	for i := range 4 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/old/s/"+string(rune('a'+i)), []string{"Go", "Swift"}, 1, old))
	}
	weights := []interest.Weight{
		{TagID: 1, Tag: "Go", Value: 8000}, {TagID: 2, Tag: "Rust", Value: 6000},
		{TagID: 3, Tag: "AWS", Value: 4000}, {TagID: 4, Tag: "Docker", Value: 2000},
	}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 空きは1枠。回数の多い Kotlin だけを似たタグにし、いいねの多い Swift は人気の記事としてそのうしろ
	want := []string{
		"https://qiita.com/go", "https://qiita.com/rust", "https://qiita.com/aws", "https://qiita.com/docker",
		"https://qiita.com/kotlin", "https://qiita.com/swift", "https://qiita.com/java",
	}
	if u := urls(got.QiitaArticles); !slices.Equal(u, want) {
		t.Fatalf("Qiita = %v", u)
	}
}

func TestServiceGetFromDBDoesNotSearchSimilarWhenFiveTagsExist(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	old := dbNow.Add(-10 * 24 * time.Hour)
	items := []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/swift", []string{"Swift"}, 1, hour),
		testArticle(article.SourceQiita, "https://qiita.com/java", []string{"Java"}, 100, hour),
	}
	for i := range 4 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/old/s/"+string(rune('a'+i)), []string{"Go", "Swift"}, 1, old))
	}
	// 上位5つが埋まっている。6番目の Swift は共起が多くても、似たタグとしては探さない
	weights := []interest.Weight{
		{TagID: 1, Tag: "Go", Value: 6000}, {TagID: 2, Tag: "Rust", Value: 5000},
		{TagID: 3, Tag: "AWS", Value: 4000}, {TagID: 4, Tag: "Docker", Value: 3000},
		{TagID: 5, Tag: "React", Value: 2000}, {TagID: 6, Tag: "Swift", Value: 1000},
	}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 共起は調べない。Swift は人気の記事として入り、6番目の興味度が付くので点数で Java より前に並ぶ
	want := []string{"https://qiita.com/go", "https://qiita.com/swift", "https://qiita.com/java"}
	if u := urls(got.QiitaArticles); !slices.Equal(u, want) {
		t.Fatalf("Qiita = %v", u)
	}
	if len(repo.cooccurCalls) != 0 {
		t.Fatalf("cooccur calls = %+v", repo.cooccurCalls)
	}
}

func TestServiceGetFromDBAllowsZeroInterestAsSimilar(t *testing.T) {
	hour := dbNow.Add(-time.Hour)
	old := dbNow.Add(-10 * 24 * time.Hour)
	items := []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 0, hour),
		testArticle(article.SourceQiita, "https://qiita.com/rust", []string{"Rust"}, 1, hour),
		testArticle(article.SourceQiita, "https://qiita.com/java", []string{"Java"}, 100, hour),
	}
	for i := range 3 {
		items = append(items, testArticle(article.SourceQiita, "https://qiita.com/old/"+string(rune('a'+i)), []string{"Go", "Rust"}, 1, old))
	}
	weights := []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}, {TagID: 2, Tag: "Rust", Value: 0}}
	repo := &memoryArticles{items: items}
	got, err := dbService(t, weights, repo).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// 興味度0は行が無いのと同じ。似たタグにして、いいねの多い人気の記事より前に置く
	want := []string{"https://qiita.com/go", "https://qiita.com/rust", "https://qiita.com/java"}
	if u := urls(got.QiitaArticles); !slices.Equal(u, want) {
		t.Fatalf("Qiita = %v", u)
	}
	if len(repo.cooccurCalls) != 1 || slices.Contains(repo.cooccurCalls[0].exclude, "rust") {
		t.Fatalf("cooccur = %+v", repo.cooccurCalls)
	}
}

func TestServiceGetFromDBReturnsCooccurrenceError(t *testing.T) {
	dbErr := errors.New("db down")
	repo := &memoryArticles{cooccurErr: dbErr}
	weights := []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}
	if _, err := dbService(t, weights, repo).Get(context.Background(), 1); !errors.Is(err, dbErr) {
		t.Fatalf("err = %v", err)
	}
	if len(repo.popularCalls) != 0 {
		t.Fatalf("popular calls = %+v", repo.popularCalls)
	}
}

// failNthTagged は、n回目のタグ検索だけ失敗させる。
type failNthTagged struct {
	memoryArticles
	n, at int
	fail  error
}

func (f *failNthTagged) ListTagged(ctx context.Context, source string, tags []string, since time.Time, limit int) ([]article.Article, error) {
	f.n++
	if f.n == f.at {
		return nil, f.fail
	}
	return f.memoryArticles.ListTagged(ctx, source, tags, since, limit)
}

func TestServiceGetFromDBReturnsSimilarTaggedError(t *testing.T) {
	old := dbNow.Add(-10 * 24 * time.Hour)
	dbErr := errors.New("db down")
	repo := &failNthTagged{at: 3, fail: dbErr}
	repo.items = []article.Article{
		testArticle(article.SourceQiita, "https://qiita.com/old/a", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/b", []string{"Go", "Rust"}, 1, old),
		testArticle(article.SourceQiita, "https://qiita.com/old/c", []string{"Go", "Rust"}, 1, old),
	}
	s := dbService(t, []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}}, nil)
	s.Articles = repo
	if _, err := s.Get(context.Background(), 1); !errors.Is(err, dbErr) {
		t.Fatalf("err = %v", err)
	}
}
