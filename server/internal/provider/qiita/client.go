// Package qiita は Qiita API v2 からタグ別の記事を取得し、共通の記事へ変換する。
// Qiitaの応答型はこのパッケージの外へ出さない。
package qiita

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/article"
	"github.com/H4aruki/MyTechPulse/server/internal/provider"
)

const (
	providerName   = "qiita"
	articleHost    = "qiita.com"
	defaultBaseURL = "https://qiita.com"
	defaultPerPage = 20
	maxPerPage     = 100 // Qiita APIが受け付ける上限
)

// Client はQiitaのタグ別記事一覧を取得する。provider.Client を満たす。
type Client struct {
	Fetcher provider.JSONFetcher
	Token   string
	BaseURL string // 空なら https://qiita.com。テストでだけ差し替える
	PerPage int    // 0なら20。1〜100
}

var _ provider.Client = Client{}

// New は本番の接続先を使うクライアントを返す。
func New(fetcher provider.JSONFetcher, token string) Client {
	return Client{Fetcher: fetcher, Token: token, BaseURL: defaultBaseURL, PerPage: defaultPerPage}
}

// item はQiita応答のうち使う項目だけ。欠落を検出するため、必須項目は零値と区別できる型にする。
type item struct {
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	LikesCount *int      `json:"likes_count"`
	CreatedAt  string    `json:"created_at"`
	Tags       []itemTag `json:"tags"`
}

type itemTag struct {
	Name string `json:"name"`
}

// Search は tag を持つ記事を、Qiitaが返す順のまま最大 PerPage 件返す。
func (c Client) Search(ctx context.Context, tag string) ([]article.Article, error) {
	tag = strings.TrimSpace(tag)
	perPage := c.PerPage
	if perPage == 0 {
		perPage = defaultPerPage
	}
	if tag == "" || tag == "." || tag == ".." || perPage < 1 || perPage > maxPerPage {
		return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidRequest}, providerName)
	}
	base := c.BaseURL
	if base == "" {
		base = defaultBaseURL
	}

	// タグは1つのパス要素としてエスケープし、"/" や ".." でパスを変えられないようにする。
	endpoint := strings.TrimRight(base, "/") + "/api/v2/tags/" + url.PathEscape(tag) + "/items"
	q := url.Values{}
	q.Set("page", "1")
	q.Set("per_page", strconv.Itoa(perPage))

	headers := http.Header{}
	if c.Token != "" {
		headers.Set("Authorization", "Bearer "+c.Token)
	}

	var items []item
	if err := c.Fetcher.GetJSON(ctx, endpoint+"?"+q.Encode(), headers, &items); err != nil {
		return nil, provider.Annotate(err, providerName)
	}

	articles := make([]article.Article, 0, len(items))
	for _, it := range items {
		a, ok := convert(it)
		if !ok {
			return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidResponse}, providerName)
		}
		articles = append(articles, a)
	}
	return articles, nil
}

func convert(it item) (article.Article, bool) {
	if it.LikesCount == nil || len(it.Tags) == 0 {
		return article.Article{}, false
	}
	u, err := url.Parse(it.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() != articleHost || u.User != nil {
		return article.Article{}, false
	}
	created, err := time.Parse(time.RFC3339, it.CreatedAt)
	if err != nil {
		return article.Article{}, false
	}
	tags := make([]string, 0, len(it.Tags))
	for _, t := range it.Tags {
		tags = append(tags, t.Name)
	}
	a, err := article.Validate(article.Article{
		Source:      article.SourceQiita,
		Title:       it.Title,
		URL:         it.URL,
		Tags:        tags,
		Likes:       *it.LikesCount,
		PublishedAt: created,
	})
	if err != nil {
		return article.Article{}, false
	}
	return a, true
}
