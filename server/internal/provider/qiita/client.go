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
	maxPage        = 100 // 検索のページ番号の上限

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
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	URL        string    `json:"url"`
	LikesCount *int      `json:"likes_count"`
	CreatedAt  string    `json:"created_at"`
	Tags       []itemTag `json:"tags"`
	User       *itemUser `json:"user"`
}

type itemUser struct {
	ID string `json:"id"`
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

// Page は ListCreated の1ページ分。
type Page struct {
	Articles []article.Article
	// RateRemaining は応答の Rate-Remaining（このトークンの、この時間枠の残り回数）。読めなければ -1。
	RateRemaining int
}

// FullPage は、このページが満杯（次のページがありうる）かを返す。
func (c Client) FullPage(p Page) bool { return len(p.Articles) >= c.perPage() }

// ListCreated は、UTCの日付 day に作られた記事を、新しい順の page ページ目（1始まり）で返す（定期一括取得用）。
// 記事ID（external_id）と著者も読む。タグが0件の記事も返す。必須項目が欠けた記事があれば応答不正にする。
// Qiitaが日付をどのタイムゾーンで解釈しても、連続する日付をたどれば取りこぼさない（重なりは呼び出し側が upsert で吸収する）。
func (c Client) ListCreated(ctx context.Context, day time.Time, page int) (Page, error) {
	perPage := c.perPage()
	if day.IsZero() || page < 1 || page > maxPage || perPage < 1 || perPage > maxPerPage {
		return Page{}, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidRequest}, providerName)
	}
	from := day.UTC().Truncate(24 * time.Hour)
	endpoint := strings.TrimRight(c.baseURL(), "/") + "/api/v2/items"
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("per_page", strconv.Itoa(perPage))
	q.Set("query", "created:>="+from.Format("2006-01-02")+" created:<"+from.AddDate(0, 0, 1).Format("2006-01-02"))

	var items []item
	header, err := c.Fetcher.GetJSONHeader(ctx, endpoint+"?"+q.Encode(), c.headers(), &items)
	out := Page{RateRemaining: rateRemaining(header)}
	if err != nil {
		return out, provider.Annotate(err, providerName)
	}
	out.Articles = make([]article.Article, 0, len(items))
	for _, it := range items {
		a, ok := convertBatch(it)
		if !ok {
			return out, provider.Annotate(&provider.Error{Kind: provider.ErrInvalidResponse}, providerName)
		}
		out.Articles = append(out.Articles, a)
	}
	return out, nil
}

func (c Client) perPage() int {
	if c.PerPage == 0 {
		return defaultPerPage
	}
	return c.PerPage
}

func (c Client) headers() http.Header {
	headers := http.Header{}
	if c.Token != "" {
		headers.Set("Authorization", "Bearer "+c.Token)
	}
	return headers
}

// rateRemaining は Rate-Remaining ヘッダーを整数で返す。無い・不正なら -1。
func rateRemaining(h http.Header) int {
	if h == nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(h.Get("Rate-Remaining")))
	if err != nil || n < 0 {
		return -1
	}
	return n
}

func (c Client) baseURL() string {
	if c.BaseURL == "" {
		return defaultBaseURL
	}
	return c.BaseURL
}

func (c Client) fetch(ctx context.Context, endpoint string, q url.Values) ([]article.Article, error) {
	var items []item
	if err := c.Fetcher.GetJSON(ctx, endpoint+"?"+q.Encode(), c.headers(), &items); err != nil {
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

// convertBatch は定期一括取得用の変換。記事IDと著者が必須で、タグは0件でもよい。
func convertBatch(it item) (article.Article, bool) {
	if strings.TrimSpace(it.ID) == "" || it.User == nil || strings.TrimSpace(it.User.ID) == "" {
		return article.Article{}, false
	}
	a, ok := build(it)
	if !ok {
		return article.Article{}, false
	}
	a.ExternalID = it.ID
	a.Author = it.User.ID
	return a, true
}

func convert(it item) (article.Article, bool) {
	if len(it.Tags) == 0 {
		return article.Article{}, false
	}
	return build(it)
}

func build(it item) (article.Article, bool) {
	if it.LikesCount == nil {
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
