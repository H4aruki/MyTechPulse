// Package qiita は Qiita API v2 からタグ別の記事と人気の記事を取得し、共通の記事へ変換する。
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

	// 人気の記事(Popular)の取得条件。Qiita API v2 には「いいね数の順」に並べる指定が無く、
	// 検索結果は新しい順に返る。そのため、ストック数の下限で反響のあった記事に絞ってから、
	// 推薦処理がいいね数の順に並べる(検索条件の likes: は効かず、stocks: は効く)。
	popularMinStocks = 10
	// 人気の記事は本文が長く、20件だと応答が PROVIDER_MAX_BYTES(約2MB)を超えやすいため10件にする
	// (2026-10-19に実測。ストック5以上の20件で約2.3MB、10件で約1.2MB)。
	popularPerPage = 10
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
	// Qiitaのタグ名には空白がないため、"Spring Boot" は "SpringBoot" で問い合わせる。
	tag = strings.Join(strings.Fields(tag), "")
	perPage := c.PerPage
	if perPage == 0 {
		perPage = defaultPerPage
	}
	if tag == "" || tag == "." || tag == ".." || perPage < 1 || perPage > maxPerPage {
		return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidRequest}, providerName)
	}
	base := c.baseURL()

	// タグは1つのパス要素としてエスケープし、"/" や ".." でパスを変えられないようにする。
	endpoint := strings.TrimRight(base, "/") + "/api/v2/tags/" + url.PathEscape(tag) + "/items"
	q := url.Values{}
	q.Set("page", "1")
	q.Set("per_page", strconv.Itoa(perPage))
	return c.fetch(ctx, endpoint, q)
}

// Popular は、タグを問わず since 以降に投稿され、ストック数が popularMinStocks 以上の記事を、
// Qiitaが返す順(新しい順)のまま最大 popularPerPage 件返す。並べ替えは呼び出し側が行う。
// 期間はQiitaの検索条件で日付単位に絞る。UTCの日付で指定するので、
// QiitaがUTC・日本時間のどちらで解釈しても since より前から含まれる(最終的な絞り込みは呼び出し側)。
func (c Client) Popular(ctx context.Context, since time.Time) ([]article.Article, error) {
	if since.IsZero() {
		return nil, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidRequest}, providerName)
	}
	base := c.baseURL()
	endpoint := strings.TrimRight(base, "/") + "/api/v2/items"
	q := url.Values{}
	q.Set("page", "1")
	q.Set("per_page", strconv.Itoa(popularPerPage))
	q.Set("query", "created:>="+since.UTC().Format("2006-01-02")+" stocks:>="+strconv.Itoa(popularMinStocks))
	return c.fetch(ctx, endpoint, q)
}

func (c Client) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return c.BaseURL
}

func (c Client) fetch(ctx context.Context, endpoint string, q url.Values) ([]article.Article, error) {
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
