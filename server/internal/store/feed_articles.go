package store

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/interest"
	"github.com/H4aruki/MyTechPulse/server/internal/recommendation"
	"github.com/H4aruki/MyTechPulse/server/internal/store/dbgen"
)

// FeedArticles は、定期一括取得で貯めた記事をフィードの候補として読む（Issue #66 段取り2）。
type FeedArticles struct {
	q *dbgen.Queries
}

var _ recommendation.ArticleRepository = (*FeedArticles)(nil)

func NewFeedArticles(pool *pgxpool.Pool) *FeedArticles { return &FeedArticles{q: dbgen.New(pool)} }

// ListTagged は、tags のどれかが付いた source の記事を新しい順に、最大 limit 件返す。
// タグは interest.NormalizeTag と同じく、前後の空白を除いた小文字で比べる。
func (s *FeedArticles) ListTagged(ctx context.Context, source string, tags []string, since time.Time, limit int) ([]article.Article, error) {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		if key := interest.NormalizeTag(tag); key != "" {
			normalized = append(normalized, key)
		}
	}
	if len(normalized) == 0 || limit <= 0 {
		return []article.Article{}, nil
	}
	rows, err := s.q.ListTaggedArticles(ctx, dbgen.ListTaggedArticlesParams{
		Source: strings.ToLower(source), Since: tstz(since), MaxRows: clampLimit(limit), Tags: normalized,
	})
	if err != nil {
		return nil, errors.New("store: フィードの記事を取得できません")
	}
	out := make([]article.Article, 0, len(rows))
	for _, r := range rows {
		out = append(out, feedArticle(r.Source, r.Title, r.Url, r.AuthorName, r.Likes, r.PublishedAt, r.Tags))
	}
	return out, nil
}

// ListPopular は、タグを問わず source の記事をいいね数の多い順に、最大 limit 件返す。
func (s *FeedArticles) ListPopular(ctx context.Context, source string, since time.Time, limit int) ([]article.Article, error) {
	if limit <= 0 {
		return []article.Article{}, nil
	}
	rows, err := s.q.ListPopularArticles(ctx, dbgen.ListPopularArticlesParams{
		Source: strings.ToLower(source), Since: tstz(since), MaxRows: clampLimit(limit),
	})
	if err != nil {
		return nil, errors.New("store: フィードの記事を取得できません")
	}
	out := make([]article.Article, 0, len(rows))
	for _, r := range rows {
		out = append(out, feedArticle(r.Source, r.Title, r.Url, r.AuthorName, r.Likes, r.PublishedAt, r.Tags))
	}
	return out, nil
}

// feedArticle は、DBの行を記事にする。DBの提供元（'qiita'・'zenn'）を、画面と推薦で使う表記に戻す。
func feedArticle(source, title, url, author string, likes int32, published pgtype.Timestamptz, tags []string) article.Article {
	a := article.Article{Title: title, URL: url, Author: author, Likes: int(likes), PublishedAt: published.Time, Tags: tags}
	switch source {
	case "qiita":
		a.Source = article.SourceQiita
	case "zenn":
		a.Source = article.SourceZenn
	default:
		a.Source = source // 不明な値は article.Validate で落とす
	}
	if a.Tags == nil {
		a.Tags = []string{}
	}
	return a
}

func clampLimit(n int) int32 {
	if n > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(n)
}
