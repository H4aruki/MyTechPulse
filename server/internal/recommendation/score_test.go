package recommendation

import (
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
)

func testArticle(source, url string, tags []string, likes int, published time.Time) article.Article {
	return article.Article{Source: source, Title: "fixed", URL: url, Tags: tags, Likes: likes, PublishedAt: published}
}

func TestScoreArticlesUsesProviderRulesAndUniqueTags(t *testing.T) {
	weights := []interest.Weight{{Tag: "Go", Value: 5000}, {Tag: "PostgreSQL", Value: 2500}}
	got := ScoreArticles(weights, []article.Article{testArticle(article.SourceQiita, "https://qiita.com/a", []string{"go", "GO"}, 4, time.Time{}), testArticle(article.SourceZenn, "https://zenn.dev/a", []string{"go", "postgresql"}, 99, time.Time{}), testArticle(article.SourceQiita, "https://qiita.com/unknown", []string{"rust"}, 100, time.Time{})})
	want := map[string]int64{"https://qiita.com/a": 25000, "https://zenn.dev/a": 7500, "https://qiita.com/unknown": 0}
	for _, item := range got {
		if want[item.Article.URL] != item.Score {
			t.Errorf("%s = %d", item.Article.URL, item.Score)
		}
	}
}

func TestScoreArticlesSortsDeterministically(t *testing.T) {
	now := time.Now()
	input := []article.Article{testArticle(article.SourceQiita, "https://qiita.com/b", []string{"go"}, 0, now), testArticle(article.SourceQiita, "https://qiita.com/a", []string{"go"}, 0, now)}
	for n := 0; n < 100; n++ {
		got := ScoreArticles([]interest.Weight{{Tag: "go", Value: 10}}, input)
		if got[0].Article.URL != "https://qiita.com/a" {
			t.Fatalf("iteration %d: %s", n, got[0].Article.URL)
		}
	}
}
