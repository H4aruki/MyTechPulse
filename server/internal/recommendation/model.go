// Package recommendation scores and assembles article feeds.
package recommendation

import "github.com/H4aruki/MyTechPulse/server/internal/article"

type ScoredArticle struct {
	Article article.Article
	Score   int64
}

type Warning struct {
	Provider string `json:"provider" doc:"記事提供元" example:"qiita"`
	Code     string `json:"code" doc:"安定した失敗コード" example:"provider_unavailable"`
}

type FeedArticle struct {
	Title       string   `json:"title" doc:"記事タイトル"`
	URL         string   `json:"url" doc:"記事URL"`
	Source      string   `json:"source" enum:"Qiita,Zenn" doc:"記事提供元"`
	Tags        []string `json:"tags" doc:"記事タグ"`
	Likes       int      `json:"likes" doc:"いいね数"`
	PublishedAt string   `json:"published_at" format:"date-time" doc:"公開日時"`
}

type Result struct {
	QiitaArticles []article.Article
	ZennArticles  []article.Article
	Warnings      []Warning
}

func feedArticles(items []article.Article) []FeedArticle {
	out := make([]FeedArticle, 0, len(items))
	for _, item := range items {
		out = append(out, FeedArticle{Title: item.Title, URL: item.URL, Source: item.Source, Tags: item.Tags, Likes: item.Likes, PublishedAt: item.PublishedAt.UTC().Format("2006-01-02T15:04:05Z07:00")})
	}
	return out
}
