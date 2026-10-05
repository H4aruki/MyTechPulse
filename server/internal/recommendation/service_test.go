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
	mu    sync.Mutex
	byTag map[string][]article.Article
	err   error
	calls []string
}

func (f *fixedClient) Search(_ context.Context, tag string) ([]article.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, tag)
	return append([]article.Article(nil), f.byTag[tag]...), f.err
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

func TestServiceGetReturnsEmptyForNoInterests(t *testing.T) {
	s := Service{Interests: &memoryInterests{}, Clock: fixedClock{time.Now()}}
	got, err := s.Get(context.Background(), 1)
	if err != nil || len(got.QiitaArticles) != 0 || len(got.ZennArticles) != 0 {
		t.Fatalf("got %#v %v", got, err)
	}
}
