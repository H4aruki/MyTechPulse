package recommendation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type memoryInterests struct {
	weights []interest.Weight
	err     error
	clicked []string
}

func (m *memoryInterests) List(context.Context, int64) ([]interest.Weight, error) {
	return append([]interest.Weight(nil), m.weights...), m.err
}
func (m *memoryInterests) UpdateForClick(_ context.Context, _ int64, tags []string) ([]interest.Weight, error) {
	m.clicked = append([]string(nil), tags...)
	return nil, m.err
}

type fixedClient struct {
	mu           sync.Mutex
	byTag        map[string][]article.Article
	popular      []article.Article
	err          error
	popularErr   error
	calls        []string
	popularSince []time.Time
}

func (f *fixedClient) Search(_ context.Context, tag string) ([]article.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tag)
	return append([]article.Article(nil), f.byTag[tag]...), f.err
}

// Popular は人気の記事を返す。popularErr が無ければ、Search と同じ err を返す(提供元ごと落ちている想定)。
func (f *fixedClient) Popular(_ context.Context, since time.Time) ([]article.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.popularSince = append(f.popularSince, since)
	err := f.err
	if f.popularErr != nil {
		err = f.popularErr
	}
	return append([]article.Article(nil), f.popular...), err
}
func mustArticle(source, url string, tags []string, published time.Time) article.Article {
	return article.Article{Source: source, Title: "fixed", URL: url, Tags: tags, Likes: 1, PublishedAt: published}
}

func TestServiceGetFiltersMergesAndFallsBackToFetchedOldZenn(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	old := now.Add(-20 * 24 * time.Hour)
	fresh := now.Add(-24 * time.Hour)
	interests := &memoryInterests{weights: []interest.Weight{{TagID: 1, Tag: "Go", Value: 5000}, {TagID: 2, Tag: "Rust", Value: 1000}}}
	qiita := &fixedClient{byTag: map[string][]article.Article{"Go": {mustArticle(article.SourceQiita, "https://qiita.com/a", []string{"go"}, fresh), mustArticle(article.SourceQiita, "https://qiita.com/old", []string{"go"}, old)}}}
	zenn := &fixedClient{byTag: map[string][]article.Article{"Go": {mustArticle(article.SourceZenn, "https://zenn.dev/old", []string{"go"}, old)}, "Rust": {mustArticle(article.SourceZenn, "https://zenn.dev/old", []string{"rust"}, old)}}}
	s := Service{Interests: interests, Providers: ProviderSet{Qiita: qiita, Zenn: zenn}, Clock: fixedClock{now}, FeedTimeout: time.Second}
	got, err := s.Get(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 1 || got.QiitaArticles[0].URL != "https://qiita.com/a" {
		t.Fatalf("Qiita = %#v", got.QiitaArticles)
	}
	if len(got.ZennArticles) != 1 || len(got.ZennArticles[0].Tags) != 2 {
		t.Fatalf("Zenn fallback/merge = %#v", got.ZennArticles)
	}
	if len(qiita.calls) != 2 || len(zenn.calls) != 2 {
		t.Fatalf("calls: Qiita %v Zenn %v", qiita.calls, zenn.calls)
	}
}

func TestServicePartialAndTotalFailures(t *testing.T) {
	interests := &memoryInterests{weights: []interest.Weight{{TagID: 1, Tag: "Go", Value: 1}}}
	broken := &fixedClient{err: errors.New("offline")}
	ok := &fixedClient{byTag: map[string][]article.Article{}}
	s := Service{Interests: interests, Providers: ProviderSet{Qiita: broken, Zenn: ok}, Clock: fixedClock{time.Now()}}
	got, err := s.Get(context.Background(), 1)
	if err != nil || len(got.Warnings) != 1 || got.Warnings[0].Provider != "qiita" {
		t.Fatalf("result %#v err %v", got, err)
	}
	s.Providers.Zenn = broken
	if _, err = s.Get(context.Background(), 1); !errors.Is(err, ErrAllProvidersFailed) {
		t.Fatalf("got %v", err)
	}
}

func TestRecordClickDelegatesAndRepositoryCollisionStopsProviders(t *testing.T) {
	repo := &memoryInterests{weights: []interest.Weight{{TagID: 1, Tag: "Go"}, {TagID: 2, Tag: " go "}}}
	client := &fixedClient{}
	s := Service{Interests: repo, Providers: ProviderSet{Qiita: client, Zenn: client}}
	if _, err := s.Get(context.Background(), 1); !errors.Is(err, interest.ErrNormalizedTagCollision) {
		t.Fatalf("got %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatal("provider called despite invalid interests")
	}
	if err := s.RecordClick(context.Background(), 1, []string{"go"}); err != nil || len(repo.clicked) != 1 {
		t.Fatalf("click %v %v", repo.clicked, err)
	}
}

func TestServiceGetWithNoPositiveInterestsUsesOnlyPopular(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	q := mustArticle(article.SourceQiita, "https://qiita.com/popular", []string{"Kotlin"}, now.Add(-time.Hour))
	z := mustArticle(article.SourceZenn, "https://zenn.dev/popular", nil, now.Add(-time.Hour))
	qiita, zenn := &fixedClient{popular: []article.Article{q}}, &fixedClient{popular: []article.Article{z}}
	// 行が無い利用者と、値がすべて0の利用者は同じ扱い
	for _, weights := range [][]interest.Weight{nil, {{TagID: 1, Tag: "Go", Value: 0}}} {
		qiita.calls, zenn.calls, qiita.popularSince, zenn.popularSince = nil, nil, nil, nil
		s := Service{Interests: &memoryInterests{weights: weights}, Providers: ProviderSet{Qiita: qiita, Zenn: zenn}, Clock: fixedClock{now}}
		got, err := s.Get(context.Background(), 1)
		if err != nil || len(got.QiitaArticles) != 1 || len(got.ZennArticles) != 1 || len(got.Warnings) != 0 {
			t.Fatalf("got %#v %v", got, err)
		}
		if len(qiita.calls) != 0 || len(zenn.calls) != 0 || len(qiita.popularSince) != 1 || len(zenn.popularSince) != 1 {
			t.Fatalf("calls: Qiita %v/%v Zenn %v/%v", qiita.calls, qiita.popularSince, zenn.calls, zenn.popularSince)
		}
	}
}

func TestServiceGetWithNoInterestsAndNoProvidersFails(t *testing.T) {
	s := Service{Interests: &memoryInterests{}, Clock: fixedClock{time.Now()}}
	if _, err := s.Get(context.Background(), 1); !errors.Is(err, ErrAllProvidersFailed) {
		t.Fatalf("got %v", err)
	}
}
