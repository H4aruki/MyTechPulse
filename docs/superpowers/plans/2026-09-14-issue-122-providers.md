# Issue 122 Article Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Qiitaと現行Zenn APIから記事を安全に取得し、外部応答を検証して共通Articleへ正規化する。

**Architecture:** 提供元ごとにHTTPクライアントと変換を分離し、標準 `http.Client` を注入する。外部失敗は分類済みエラーへ変換し、応答上限、期限、redirect拒否、URL検証を共通化する。

**Tech Stack:** Go 1.26、標準net/http、encoding/json

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- Zennは現行 `https://zenn.dev/api/articles` を使用し、RSSへ切り替えない
- Qiitaは `https://qiita.com/api/v2/tags/{tag}/items` だけを使用する
- 固定fixtureテスト以外で外部サービスへ接続しない
- redirectは追従しない
- 応答bodyは設定した最大byte数を超えて読まない
- timeout、接続失敗、502/503/504だけを期限内で最大1回再試行し、429と他4xxは再試行しない
- Authorization、検索語、外部応答bodyをログへ出さない
- 記事URLは `https` と許可hostだけを受理する
- 一方の提供元失敗を、もう一方の成功まで失敗へ変えない

---

## File Map

- Create: `server/internal/article/model.go`, `model_test.go` — 共通記事型
- Create: `server/internal/provider/model.go`, `errors.go` — client境界とエラー
- Create: `server/internal/provider/httpclient.go`, `httpclient_test.go` — 安全な共通HTTP
- Create: `server/internal/provider/qiita/client.go`, `client_test.go` — Qiita取得
- Create: `server/internal/provider/zenn/client.go`, `client_test.go` — 現行Zenn API取得
- Create: `server/internal/provider/metrics.go`, `metrics_test.go` — 秘密値を含まない観測
- Read: `testdata/compatibility/qiita_articles.json`, `zenn_articles.json` — #118共有fixture

### Task 1: 共通型と分類済みエラーを定義する

**Files:**
- Create: `server/internal/article/model.go`, `model_test.go`
- Create: `server/internal/provider/model.go`, `errors.go`

**Interfaces:**

```go
type Article struct {
    Source string
    Title string
    URL string
    Tags []string
    Likes int
    PublishedAt time.Time
}
type Client interface { Search(context.Context, string) ([]article.Article, error) }
var (
    ErrTimeout = errors.New("provider timeout")
    ErrRateLimited = errors.New("provider rate limited")
    ErrInvalidResponse = errors.New("provider invalid response")
    ErrUnavailable = errors.New("provider unavailable")
)
```

- [ ] **Step 1: 型の不変条件テストを書く**

Sourceは `Qiita`/`Zenn`、Title/URLは非空、Likesは0以上、PublishedAtはUTCへ正規化するvalidatorを表形式でテストする。

- [ ] **Step 2: 未実装の失敗を確認する**

```bash
cd server
go test ./internal/article ./internal/provider -v
```

- [ ] **Step 3: `article.Validate` とエラー型を実装する**

外部status code、retry時刻、提供元名は構造化フィールドとして保持するが、bodyやtokenは保持しない。HTTP 429は `errors.Is(err, ErrRateLimited)` で判定可能にする。

- [ ] **Step 4: テストしてコミットする**

```bash
cd server
go test ./internal/article ./internal/provider -race -v
git add server/internal/article server/internal/provider/model.go server/internal/provider/errors.go
git commit -m "feat(news): 記事提供元の共通契約を定義" -m "Refs #122"
```

### Task 2: 安全な外部HTTP境界を作る

**Files:**
- Create: `server/internal/provider/httpclient.go`, `httpclient_test.go`

**Interfaces:**

```go
type HTTPClient interface { Do(*http.Request) (*http.Response, error) }
type JSONFetcher struct {
    Client HTTPClient
    MaxBytes int64
    AllowedHosts map[string]struct{}
}
func (f JSONFetcher) GetJSON(ctx context.Context, rawURL string, headers http.Header, out any) error
```

- [ ] **Step 1: 防御条件の失敗テストを書く**

`httptest.Server` とfake transportで、HTTP URL、許可以外host、redirect 302、429、500、Content-Length超過、chunked body超過、不正JSON、期限超過を個別にテストする。上限は64byte、65byteは `ErrInvalidResponse` とする。接続失敗と502/503/504は初回失敗後に1回だけ再試行し、成功・429・400・context残時間不足は1回で終わることも確認する。

- [ ] **Step 2: 未実装の失敗を確認する**

```bash
cd server
go test ./internal/provider -run TestJSONFetcher -v
```

- [ ] **Step 3: 共通fetcherを実装する**

URLを `net/url.Parse` しschemeとhostnameを完全一致で確認する。`io.LimitReader(response.Body, MaxBytes+1)` を読み、上限超過ならdecodeしない。200だけをdecodeし、429の `Retry-After` は秒またはHTTP-dateとして解析する。context deadline/cancelは分類して返す。再試行対象だけ100ms timerを `select` でcontextと待ち、2回目で終了する。各試行のresponse bodyをその場でcloseする。

実clientは次で作る。

```go
&http.Client{
    Timeout: cfg.ProviderTimeout,
    CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
        return http.ErrUseLastResponse
    },
}
```

- [ ] **Step 4: テストしてコミットする**

```bash
cd server
go test ./internal/provider -race -v
git add server/internal/provider/httpclient.go server/internal/provider/httpclient_test.go
git commit -m "feat(news): 外部HTTP応答の安全境界を追加" -m "Refs #122"
```

### Task 3: Qiitaクライアントを実装する

**Files:**
- Create: `server/internal/provider/qiita/client.go`, `client_test.go`
- Read: `testdata/compatibility/qiita_articles.json`

**Interfaces:**

```go
type Client struct { Fetcher provider.JSONFetcher; Token string; BaseURL string; PerPage int }
func (c Client) Search(ctx context.Context, tag string) ([]article.Article, error)
```

- [ ] **Step 1: 固定fixtureでrequest契約テストを書く**

server側でmethod GET、path `/api/v2/tags/Go/items`、`Authorization: Bearer synthetic-token`、query `per_page=20`、`page=1` を検査する。`Sass/SCSS` と `../` が1つのpath segmentとしてescapeされることも確認する。fixtureを返し、共通ArticleのURL、タグ、likes、UTC時刻を比較する。

- [ ] **Step 2: 境界テストを書く**

空Tag、PerPage 0/101は通信前に失敗、Qiita以外のURLは不正、欠落title/url/tags、負のlikes、不正created_atは `ErrInvalidResponse` とする。rate-limit分類を保持する。

- [ ] **Step 3: 実装する**

BaseURL既定値は `https://qiita.com`、PerPage既定値は20。URLは `strings.TrimRight(BaseURL, "/") + "/api/v2/tags/" + url.PathEscape(tag) + "/items"`、queryは `url.Values` で組み立てる。テストだけconstructorでfixture server URLを注入する。

- [ ] **Step 4: テストしてコミットする**

```bash
cd server
go test ./internal/provider/qiita -race -v
git add server/internal/provider/qiita
git commit -m "feat(news): Qiita記事取得をGoへ移植" -m "Refs #122"
```

### Task 4: 現行Zenn APIクライアントを実装する

**Files:**
- Create: `server/internal/provider/zenn/client.go`, `client_test.go`
- Read: `testdata/compatibility/zenn_articles.json`

**Interfaces:**

```go
type Client struct { Fetcher provider.JSONFetcher; Endpoint string; Count int }
func (c Client) Search(ctx context.Context, tag string) ([]article.Article, error)
```

- [ ] **Step 1: 現行APIの固定requestテストを書く**

method GET、path `/api/articles`、queryの `topicname=go`、`count=5` をfixture serverで検査する。`liked_count` は表示用Likes、検索TagはArticle.Tags、`path` は `https://zenn.dev` を基準に絶対URLへする。

- [ ] **Step 2: 期間と変換境界テストを書く**

relative path以外、`zenn.dev` 以外の絶対URL、欠落title/path/published_at、負のliked_countを拒否する。同一応答内の同一URLは1件へまとめる。期間絞り込みは、古い候補をfallbackへ残すため #123 のserviceで行う。

- [ ] **Step 3: 実装する**

Endpointは `https://zenn.dev/api/articles`、Countは5に固定し、RSSコードや代替endpointを入れない。topicnameは `strings.ToLower(strings.TrimSpace(tag))` とする。API応答 `{ "articles": [] }` をdecodeし、`published_at` はRFC3339NanoとしてUTC化する。0件は空成功とする。

- [ ] **Step 4: 契約テストしてコミットする**

```bash
cd server
go test ./internal/provider/zenn -run 'Test(Search|CurrentAPIContract)' -race -v
git add server/internal/provider/zenn
git commit -m "feat(news): 現行Zenn API取得をGoへ移植" -m "Refs #122"
```

Expected: request URLに `/api/articles` が含まれ、RSS文字列は生成されない。

### Task 5: 提供元の安全な観測を追加する

**Files:**
- Create: `server/internal/provider/metrics.go`, `metrics_test.go`

**Interfaces:**
- Produces: #123が組み立てるQiita/Zenn client constructor
- Produces: provider、outcome、status_class、durationだけを持つログ属性

- [ ] **Step 1: 秘密値非出力テストを書く**

Qiita token、検索Tag、応答bodyを含むfake失敗を渡してもログへ値が現れず、provider名と `timeout|rate_limited|invalid_response|unavailable|success` だけが残ることを確認する。

- [ ] **Step 2: 共通client constructorを作る**

ConfigのProviderTimeout/ProviderMaxBytesを受け、許可hostを `qiita.com`、`zenn.dev` の完全一致へ固定するconstructorを作る。mainへの注入と公開feed経路は #123 で追加する。

- [ ] **Step 3: 全検証を行う**

```bash
cd server
gofmt -w .
go vet ./...
go test ./internal/article ./internal/provider/... -race
```

- [ ] **Step 4: 統合をコミットする**

```bash
git add server/internal/provider/metrics.go server/internal/provider/metrics_test.go
git commit -m "feat(news): 記事提供元の安全な観測を追加" -m "Refs #122"
```

- [ ] **Step 5: PRを作る**

PRタイトルは `feat(news): Qiitaと現行Zenn APIの安全な取得層を実装する`。本文にZenn endpoint、期間、redirect拒否、body上限、timeout/rate-limit分類、外部接続を使わないfixture結果を記載し、`Closes #122` を付ける。人間がレビュー・マージする。
