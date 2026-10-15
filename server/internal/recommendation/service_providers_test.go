package recommendation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

var testNow = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func newService(weights []interest.Weight, qiita, zenn *fixedClient) Service {
	return Service{
		Interests:   &memoryInterests{weights: weights},
		Providers:   ProviderSet{Qiita: qiita, Zenn: zenn},
		Clock:       fixedClock{testNow},
		FeedTimeout: time.Second,
	}
}

func urls(items []article.Article) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.URL)
	}
	return out
}

func TestServiceGetFilterWindowBoundaries(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	qiita := &fixedClient{byTag: map[string][]article.Article{"go": {
		mustArticle(article.SourceQiita, "https://qiita.com/exactly5d", []string{"go"}, testNow.Add(-5*24*time.Hour)),
		mustArticle(article.SourceQiita, "https://qiita.com/over5d", []string{"go"}, testNow.Add(-5*24*time.Hour-time.Second)),
	}}}
	zenn := &fixedClient{byTag: map[string][]article.Article{"go": {
		mustArticle(article.SourceZenn, "https://zenn.dev/exactly14d", []string{"go"}, testNow.Add(-14*24*time.Hour)),
		mustArticle(article.SourceZenn, "https://zenn.dev/over14d", []string{"go"}, testNow.Add(-14*24*time.Hour-time.Second)),
	}}}
	got, err := newService(weights, qiita, zenn).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if u := urls(got.QiitaArticles); len(u) != 1 || u[0] != "https://qiita.com/exactly5d" {
		t.Fatalf("Qiita は5日以内だけ残るはず: %v", u)
	}
	// 新しい Zenn 記事があるときは、古い記事は混ざらない。
	if u := urls(got.ZennArticles); len(u) != 1 || u[0] != "https://zenn.dev/exactly14d" {
		t.Fatalf("Zenn は14日以内だけ残るはず: %v", u)
	}
}

func TestServiceGetUsesOnlyTopFiveTags(t *testing.T) {
	var weights []interest.Weight
	for i := 1; i <= 6; i++ {
		weights = append(weights, interest.Weight{TagID: int64(i), Tag: fmt.Sprintf("tag%d", i), Value: int64(i) * 1000})
	}
	qiita, zenn := &fixedClient{}, &fixedClient{}
	if _, err := newService(weights, qiita, zenn).Get(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*fixedClient{"qiita": qiita, "zenn": zenn} {
		if len(c.calls) != 5 {
			t.Fatalf("%s の呼び出し数 = %v", name, c.calls)
		}
		for _, tag := range c.calls {
			if tag == "tag1" {
				t.Fatalf("%s: 興味度が最も低い tag1 を取得してはいけない: %v", name, c.calls)
			}
		}
	}
}

func TestServiceGetRanksByScoreAndLimitsToTen(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}, {TagID: 2, Tag: "rust", Value: 1000}}
	fresh := testNow.Add(-time.Hour)
	var qiitaGo []article.Article
	for i := 0; i < 12; i++ {
		a := mustArticle(article.SourceQiita, fmt.Sprintf("https://qiita.com/go%02d", i), []string{"go"}, fresh)
		a.Likes = i
		qiitaGo = append(qiitaGo, a)
	}
	rust := mustArticle(article.SourceQiita, "https://qiita.com/rust-popular", []string{"rust"}, fresh)
	rust.Likes = 3 // 1000 × 4 = 4000。go の likes=0 (5000) 未満だが、10件の枠からは外れる。
	qiita := &fixedClient{byTag: map[string][]article.Article{"go": qiitaGo, "rust": {rust}}}
	got, err := newService(weights, qiita, &fixedClient{}).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 10 {
		t.Fatalf("提供元ごとに10件までのはず: %d件", len(got.QiitaArticles))
	}
	// 点数 = タグ重み × (likes+1)。likes が多い順に並ぶ。
	if got.QiitaArticles[0].URL != "https://qiita.com/go11" || got.QiitaArticles[9].URL != "https://qiita.com/go02" {
		t.Fatalf("順位が点数順でない: %v", urls(got.QiitaArticles))
	}
}

func TestServiceGetReturnsHealthyProviderWhenOtherIsDown(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	good := func(source, url string) *fixedClient {
		return &fixedClient{byTag: map[string][]article.Article{"go": {mustArticle(source, url, []string{"go"}, testNow.Add(-time.Hour))}}}
	}
	down := &fixedClient{err: errors.New("503")}

	got, err := newService(weights, down, good(article.SourceZenn, "https://zenn.dev/a")).Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("片方だけ失敗ならエラーにしない: %v", err)
	}
	if len(got.QiitaArticles) != 0 || len(got.ZennArticles) != 1 {
		t.Fatalf("Zenn の記事は返すはず: %#v", got)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != (Warning{Provider: "qiita", Code: "provider_unavailable"}) {
		t.Fatalf("warnings = %#v", got.Warnings)
	}

	got, err = newService(weights, good(article.SourceQiita, "https://qiita.com/a"), down).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 1 || len(got.ZennArticles) != 0 {
		t.Fatalf("Qiita の記事は返すはず: %#v", got)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Provider != "zenn" {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestServiceGetTreatsMissingProviderAsDown(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	zenn := &fixedClient{byTag: map[string][]article.Article{"go": {mustArticle(article.SourceZenn, "https://zenn.dev/a", []string{"go"}, testNow)}}}
	s := newService(weights, nil, zenn)
	s.Providers.Qiita = nil
	got, err := s.Get(context.Background(), 1)
	if err != nil || len(got.ZennArticles) != 1 || len(got.Warnings) != 1 || got.Warnings[0].Provider != "qiita" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

type hangingClient struct{}

func (hangingClient) Search(ctx context.Context, _ string) ([]article.Article, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestServiceGetTimeoutCountsAsProviderDown(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	zenn := &fixedClient{byTag: map[string][]article.Article{"go": {mustArticle(article.SourceZenn, "https://zenn.dev/a", []string{"go"}, testNow)}}}
	s := newService(weights, nil, zenn)
	s.Providers.Qiita = hangingClient{}
	s.FeedTimeout = 20 * time.Millisecond
	done := make(chan struct{})
	var got Result
	var err error
	go func() { got, err = s.Get(context.Background(), 1); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("応答しない提供元のせいで Get が終わらない")
	}
	if err != nil || len(got.ZennArticles) != 1 || len(got.Warnings) != 1 || got.Warnings[0].Provider != "qiita" {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestServiceGetInvalidArticleFromProviderIsWarning(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	broken := mustArticle(article.SourceQiita, "not-a-url", []string{"go"}, testNow)
	qiita := &fixedClient{byTag: map[string][]article.Article{"go": {broken}}}
	zenn := &fixedClient{byTag: map[string][]article.Article{"go": {mustArticle(article.SourceZenn, "https://zenn.dev/a", []string{"go"}, testNow)}}}
	got, err := newService(weights, qiita, zenn).Get(context.Background(), 1)
	if err != nil || len(got.QiitaArticles) != 0 || len(got.Warnings) != 1 || got.Warnings[0].Provider != "qiita" {
		t.Fatalf("got %#v, %v", got, err)
	}
}
