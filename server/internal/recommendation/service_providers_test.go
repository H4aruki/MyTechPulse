package recommendation

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
		if len(c.popularSince) != 0 {
			t.Fatalf("%s: 5タグあるときは人気の記事を取らない: %v", name, c.popularSince)
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

func (hangingClient) Popular(ctx context.Context, _ time.Time) ([]article.Article, error) {
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

func popularArticle(source, url string, tags []string, likes int, published time.Time) article.Article {
	a := mustArticle(source, url, tags, published)
	a.Likes = likes
	return a
}

func TestServiceGetSkipsZeroWeightTagsAndFillsWithPopular(t *testing.T) {
	weights := []interest.Weight{
		{TagID: 1, Tag: "Android", Value: 0},
		{TagID: 2, Tag: "Angular", Value: 0},
		{TagID: 3, Tag: "Go", Value: 10000},
	}
	fresh := testNow.Add(-time.Hour)
	qiita := &fixedClient{
		byTag: map[string][]article.Article{"Go": {popularArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go"}, 0, fresh)}},
		popular: []article.Article{
			popularArticle(article.SourceQiita, "https://qiita.com/p-low", []string{"Kotlin"}, 5, fresh),
			popularArticle(article.SourceQiita, "https://qiita.com/p-high", []string{"Rust"}, 50, testNow.Add(-2*time.Hour)),
			popularArticle(article.SourceQiita, "https://qiita.com/p-old", []string{"Rust"}, 999, testNow.Add(-6*24*time.Hour)),
			// 人気の記事でも、興味のあるタグが付いていれば、そのタグの重みで点数が付く
			popularArticle(article.SourceQiita, "https://qiita.com/p-go", []string{"go", "AWS"}, 3, fresh),
			// タグ検索と同じ記事は1件にまとめる
			popularArticle(article.SourceQiita, "https://qiita.com/go", []string{"Go", "Docker"}, 0, fresh),
		},
	}
	zenn := &fixedClient{
		byTag: map[string][]article.Article{"Go": {popularArticle(article.SourceZenn, "https://zenn.dev/go-old", []string{"go"}, 0, testNow.Add(-20*24*time.Hour))}},
		popular: []article.Article{
			popularArticle(article.SourceZenn, "https://zenn.dev/p1", nil, 10, fresh),
			popularArticle(article.SourceZenn, "https://zenn.dev/p2", nil, 80, fresh),
			popularArticle(article.SourceZenn, "https://zenn.dev/p-old", nil, 999, testNow.Add(-15*24*time.Hour)),
		},
	}
	got, err := newService(weights, qiita, zenn).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*fixedClient{"qiita": qiita, "zenn": zenn} {
		if len(c.calls) != 1 || c.calls[0] != "Go" {
			t.Fatalf("%s: 興味度0のタグで検索してはいけない: %v", name, c.calls)
		}
		if len(c.popularSince) != 1 {
			t.Fatalf("%s: 人気の記事は1回だけ取る: %v", name, c.popularSince)
		}
	}
	if !qiita.popularSince[0].Equal(testNow.Add(-5*24*time.Hour)) || !zenn.popularSince[0].Equal(testNow.Add(-14*24*time.Hour)) {
		t.Fatalf("since: qiita %v zenn %v", qiita.popularSince, zenn.popularSince)
	}
	// p-go: 10000×4 > go: 10000×1 > 点数0の人気の記事はいいね順。5日より古い人気の記事は外す
	if u := urls(got.QiitaArticles); strings.Join(u, " ") != "https://qiita.com/p-go https://qiita.com/go https://qiita.com/p-high https://qiita.com/p-low" {
		t.Fatalf("Qiita = %v", u)
	}
	if tags := got.QiitaArticles[1].Tags; strings.Join(tags, ",") != "Go,Docker" {
		t.Fatalf("同じ記事のタグはまとめる: %v", tags)
	}
	// タグで見つけた記事が14日以内に無いので古い記事を使い、14日以内の人気の記事をいいね順で後ろに足す
	if u := urls(got.ZennArticles); strings.Join(u, " ") != "https://zenn.dev/go-old https://zenn.dev/p2 https://zenn.dev/p1" {
		t.Fatalf("Zenn = %v", u)
	}
	if tags := got.ZennArticles[1].Tags; len(tags) != 0 {
		t.Fatalf("人気の記事に検索タグを付けてはいけない: %v", tags)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestServiceGetFourTagsUseAtMostTenCalls(t *testing.T) {
	var weights []interest.Weight
	for i := 1; i <= 4; i++ {
		weights = append(weights, interest.Weight{TagID: int64(i), Tag: fmt.Sprintf("tag%d", i), Value: int64(i) * 1000})
	}
	qiita, zenn := &fixedClient{}, &fixedClient{}
	if _, err := newService(weights, qiita, zenn).Get(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	total := len(qiita.calls) + len(qiita.popularSince) + len(zenn.calls) + len(zenn.popularSince)
	if len(qiita.calls) != 4 || len(qiita.popularSince) != 1 || len(zenn.calls) != 4 || len(zenn.popularSince) != 1 || total != 10 {
		t.Fatalf("calls: Qiita %v/%d Zenn %v/%d", qiita.calls, len(qiita.popularSince), zenn.calls, len(zenn.popularSince))
	}
}

func TestServiceGetPopularFailureIsProviderWarning(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	qiita := &fixedClient{byTag: map[string][]article.Article{"go": {mustArticle(article.SourceQiita, "https://qiita.com/a", []string{"go"}, testNow.Add(-time.Hour))}}, popularErr: errors.New("503")}
	zenn := &fixedClient{popular: []article.Article{mustArticle(article.SourceZenn, "https://zenn.dev/p", nil, testNow.Add(-time.Hour))}}
	got, err := newService(weights, qiita, zenn).Get(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.QiitaArticles) != 1 || len(got.ZennArticles) != 1 {
		t.Fatalf("取れた記事は返す: %#v", got)
	}
	if len(got.Warnings) != 1 || got.Warnings[0].Provider != "qiita" {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}

func TestServiceGetMissingProviderDuringFillIsWarning(t *testing.T) {
	weights := []interest.Weight{{TagID: 1, Tag: "go", Value: 5000}}
	zenn := &fixedClient{popular: []article.Article{mustArticle(article.SourceZenn, "https://zenn.dev/p", nil, testNow)}}
	s := newService(weights, nil, zenn)
	s.Providers.Qiita = nil
	got, err := s.Get(context.Background(), 1)
	if err != nil || len(got.ZennArticles) != 1 || len(got.Warnings) != 1 || got.Warnings[0].Provider != "qiita" {
		t.Fatalf("got %#v, %v", got, err)
	}
}
