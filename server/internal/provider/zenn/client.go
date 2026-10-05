// Package zenn は Zenn の現行記事一覧API（https://zenn.dev/api/articles）から
// トピック別の記事を取得し、共通の記事へ変換する。
// 公式仕様の無いAPIなので、必須項目の欠落や型の変化は応答不正として検出する。
// RSSなど別の取得方式へは切り替えない。Zennの応答型はこのパッケージの外へ出さない。
package zenn

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

const (
	providerName    = "zenn"
	articleOrigin   = "https://zenn.dev"
	articleHost     = "zenn.dev"
	defaultEndpoint = "https://zenn.dev/api/articles"
	defaultCount    = 5
)

// Client はZennのトピック別記事一覧を取得する。provider.Client を満たす。
type Client struct {
	Fetcher  provider.JSONFetcher
	Endpoint string // 空なら https://zenn.dev/api/articles。テストでだけ差し替える
	Count    int    // 0なら5
}

var _ provider.Client = Client{}

// New は本番の接続先を使うクライアントを返す。
func New(fetcher provider.JSONFetcher) Client {
	return Client{Fetcher: fetcher, Endpoint: defaultEndpoint, Count: defaultCount}
}

type listResponse struct {
	Articles *[]item `json:"articles"`
}

// item はZenn応答のうち使う項目だけ。欠落を検出するため、数値は零値と区別できる型にする。
type item struct {
	Title       string `json:"title"`
	Path        string `json:"path"`
	LikedCount  *int   `json:"liked_count"`
	PublishedAt string `json:"published_at"`
}

// Search は topic を持つ記事を、Zennが返す順のまま返す。
// 一覧APIは記事本来のトピックを返さないため、記事のタグには検索に使ったトピックだけを入れる。
// 期間での絞り込みは、古い候補をフォールバックに残すため呼び出し側（推薦処理）で行う。
func (c Client) Search(ctx context.Context, tag string) ([]article.Article, error) {
	// Zennのtopicnameは小文字でないとヒットしない。
	topic := strings.ToLower(strings.TrimSpace(tag))
	count := c.Count
	if count == 0 {
		count = defaultCount
	}
	if topic == "" || count < 1 {
		return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidRequest}, providerName)
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = defaultEndpoint
	}

	q := url.Values{}
	q.Set("topicname", topic)
	q.Set("count", strconv.Itoa(count))

	var resp listResponse
	if err := c.Fetcher.GetJSON(ctx, endpoint+"?"+q.Encode(), nil, &resp); err != nil {
		return nil, provider.Annotate(err, providerName)
	}
	if resp.Articles == nil {
		return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidResponse}, providerName)
	}

	articles := make([]article.Article, 0, len(*resp.Articles))
	seen := make(map[string]struct{}, len(*resp.Articles))
	for _, it := range *resp.Articles {
		a, ok := convert(it, topic)
		if !ok {
			return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidResponse}, providerName)
		}
		if _, dup := seen[a.URL]; dup {
			continue
		}
		seen[a.URL] = struct{}{}
		articles = append(articles, a)
	}
	return articles, nil
}

func convert(it item, topic string) (article.Article, bool) {
	if it.LikedCount == nil {
		return article.Article{}, false
	}
	// pathは "/" で始まる相対パスだけを受け付け、別ホストへ向けられないようにする。
	if !strings.HasPrefix(it.Path, "/") || strings.HasPrefix(it.Path, "//") || strings.Contains(it.Path, `\`) {
		return article.Article{}, false
	}
	articleURL := articleOrigin + it.Path
	u, err := url.Parse(articleURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != articleHost || u.User != nil {
		return article.Article{}, false
	}
	published, err := time.Parse(time.RFC3339Nano, it.PublishedAt)
	if err != nil {
		return article.Article{}, false
	}
	a, err := article.Validate(article.Article{
		Source:      article.SourceZenn,
		Title:       it.Title,
		URL:         articleURL,
		Tags:        []string{topic},
		Likes:       *it.LikedCount,
		PublishedAt: published,
	})
	if err != nil {
		return article.Article{}, false
	}
	return a, true
}
