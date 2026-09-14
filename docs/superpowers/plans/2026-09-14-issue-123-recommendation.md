# Issue 123 Recommendation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 興味度保存、クリック学習、記事統合、順位付けを固定小数点整数でGoへ移植し、既存データを保持したまま一貫した推薦結果を返す。

**Architecture:** 固定小数点更新を担う `interest`、採点・取得統合を担う `recommendation`、transactionを管理するrepositoryを分離する。DBロック中は興味度更新だけを行い、外部HTTP通信は行わない。

**Tech Stack:** Go 1.26、pgx v5.11.0、sqlc v1.31.1、Huma v2.39.1、PostgreSQL 17

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- `recommend.match_int` は10000倍整数のまま保存し、既存値を一括変換しない
- 計算は浮動小数を使わず、減衰 `old*8/10`、クリック加算 `+2000` を整数で行う
- Python浮動小数との差1は設計承認済みの意図的修正としてテスト名とPRへ記録する
- タグ照合は前後空白除去と小文字化を行い、同一クリック内の重複タグを1回にする
- Qiitaは5日、Zennは14日。Zennだけ期間内0件時に取得済みの古い候補へフォールバックする
- Qiita scoreは一致興味度合計×(likes+1)、Zenn scoreは一致興味度合計だけにする
- 外部HTTP通信中にDB transactionやrow lockを保持しない
- 片方の提供元が失敗しても他方が成功すれば200、両方失敗なら503にする

---

## File Map

- Create: `server/internal/interest/model.go` — Weight
- Create: `server/internal/interest/normalize.go`, `normalize_test.go` — タグ正規化
- Create: `server/internal/interest/click.go`, `click_test.go` — 固定小数点更新
- Create: `server/internal/recommendation/model.go` — ScoredArticleとfeed結果
- Create: `server/internal/recommendation/score.go`, `score_test.go` — 提供元別順位
- Create: `server/db/queries/recommendations.sql` — 興味度取得・ロック・upsert
- Create: `server/internal/store/interest.go`, `interest_integration_test.go` — transaction境界
- Create: `server/internal/recommendation/service.go`, `service_test.go` — 取得・統合・部分成功
- Create: `server/internal/recommendation/handler.go`, `handler_test.go` — feed/click API
- Modify: `server/internal/app/app.go`, `app_test.go` — 経路登録
- Modify: `server/cmd/api/main.go`, `cmd/openapi/main.go` — 実依存と仕様生成
- Modify: `server/openapi/openapi.json` — 生成契約
- Modify: `testdata/compatibility/compatibility_cases.json` — #118比較ケース

### Task 1: タグ正規化と固定小数点更新を実装する

**Files:**
- Create: `server/internal/interest/model.go`
- Create: `server/internal/interest/normalize.go`, `normalize_test.go`
- Create: `server/internal/interest/click.go`, `click_test.go`
- Modify: `testdata/compatibility/compatibility_cases.json`

**Interfaces:**

```go
const (
    Scale = int64(10000)
    DecayNumerator = int64(8)
    DecayDenominator = int64(10)
    ClickBoost = int64(2000)
)
type Weight struct { TagID int64; Tag string; Value int64 }
func NormalizeTag(string) string
func UpdateOnClick(current []Weight, clicked []string) []Weight
```

- [ ] **Step 1: 共有fixtureから境界テストを書く**

`875 -> 700`、`1725 -> 1380`、`10000 -> 8000`、clickedは減衰後に2000加算して10000へclamp、未登録clickedは2000、空白・大小文字・重複は1件、と固定する。結果順は既存TagID昇順、新規タグは正規化名昇順にする。

```go
func TestUpdateOnClickUsesApprovedIntegerRounding(t *testing.T) {
    got := UpdateOnClick([]Weight{{TagID: 1, Tag: "Go", Value: 875}}, []string{" go ", "GO"})
    want := []Weight{{TagID: 1, Tag: "Go", Value: 2700}}
    if !reflect.DeepEqual(got, want) { t.Fatalf("got %#v want %#v", got, want) }
}
```

- [ ] **Step 2: 未実装の失敗を確認する**

```bash
cd server
go test ./internal/interest -run 'Test(Normalize|Update)' -v
```

- [ ] **Step 3: 純粋関数を実装する**

`NormalizeTag` は `strings.TrimSpace` 後に `strings.ToLower`。空文字を除外する。各既存値は0〜10000へclampしてから `value*8/10`、正規化名がclicked集合にあれば2000を加え再度clampする。複数表記が同じ正規化名なら最小TagIDの表記へ統合する。

- [ ] **Step 4: Python差分をfixtureへ固定する**

JSONへ次を追加し、Goテストが全ケースを読む。既存DB値はテスト中も更新しない。

```json
"integer_rounding_cases": [
  {"stored": 875, "python_decay": 699, "go_decay": 700},
  {"stored": 1725, "python_decay": 1379, "go_decay": 1380},
  {"stored": 10000, "python_decay": 8000, "go_decay": 8000}
]
```

- [ ] **Step 5: テストしてコミットする**

```bash
cd server
go test ./internal/interest -race -v
git add server/internal/interest testdata/compatibility/compatibility_cases.json
git commit -m "feat(recommend): 固定小数点の興味更新を実装" -m "Refs #123"
```

### Task 2: 提供元別スコアと決定的な順位を実装する

**Files:**
- Create: `server/internal/recommendation/score.go`, `score_test.go`

**Interfaces:**

```go
type ScoredArticle struct { Article article.Article; Score int64 }
func ScoreArticles(weights []interest.Weight, articles []article.Article) []ScoredArticle
```

- [ ] **Step 1: QiitaとZennの差をテストする**

Go=5000、PostgreSQL=2500の記事について、Qiita likes=4なら37500、Zenn likes=99でも7500とする。未知タグだけの記事は0点。タグ重複は1回だけ加算する。

- [ ] **Step 2: 同点順をテストする**

score降順、PublishedAt降順、Provider辞書順、URL辞書順を固定し、同じ入力から常に同じ配列になることを100回確認する。

- [ ] **Step 3: 実装する**

weight mapは正規化タグをkeyにする。scoreの乗算は `math.MaxInt64/(likes+1)` を超える場合 `math.MaxInt64` へclampする。Likes負値はprovider変換で拒否済みだが、純粋関数でも0として扱う。

- [ ] **Step 4: テストしてコミットする**

```bash
cd server
go test ./internal/recommendation -race -v
git add server/internal/recommendation/score.go server/internal/recommendation/score_test.go
git commit -m "feat(recommend): 提供元別の記事順位を実装" -m "Refs #123"
```

### Task 3: 興味度repositoryとtransaction境界を実装する

**Files:**
- Create: `server/db/queries/recommendations.sql`
- Create: `server/internal/store/interest.go`, `interest_integration_test.go`
- Modify: `server/internal/store/dbgen/*` — sqlc生成物

**Interfaces:**

```go
type InterestRepository interface {
    List(context.Context, int64) ([]interest.Weight, error)
    UpdateForClick(context.Context, int64, []string) ([]interest.Weight, error)
}
```

- [ ] **Step 1: 同時更新とrollbackの結合テストを書く**

一時DBへuser/tag/recommendを作り、同一利用者への2 goroutine更新を開始barrierで揃える。完了後にlost updateが無いこと、途中で存在しないTagを注入した失敗では全行が開始前と一致することを確認する。

- [ ] **Step 2: SQLを書く**

```sql
-- name: LockUserForRecommendation :one
SELECT "user_ID" FROM "user" WHERE "user_ID" = $1 FOR UPDATE;
-- name: ListRecommendations :many
SELECT r."tag_ID", t.tag_name, r.match_int
FROM recommend r JOIN tag t ON t."tag_ID" = r."tag_ID"
WHERE r."user_ID" = $1 ORDER BY r."tag_ID";
-- name: UpsertRecommendation :exec
INSERT INTO recommend ("user_ID", "tag_ID", match_int) VALUES ($1, $2, $3)
ON CONFLICT ("user_ID", "tag_ID") DO UPDATE SET match_int = EXCLUDED.match_int;
```

- [ ] **Step 3: transactionを実装する**

`pgx.BeginTx` → user row lock → interests読込 →純粋関数計算→ #121のLockNormalizedTag/FindTagByNormalizedName/CreateTagでTag解決→upsert→commitの順にする。defer rollbackを置き、commit後のrollbackエラーは無視する。外部HTTP呼び出しはrepository interfaceに存在させない。

- [ ] **Step 4: sqlc生成と結合テストを行う**

```bash
cd server
go tool sqlc generate
go test ./internal/store -run TestInterest -race -v
```

- [ ] **Step 5: 永続化をコミットする**

```bash
git add server/db/queries/recommendations.sql server/internal/store
git commit -m "feat(recommend): 興味度更新をtransaction化" -m "Refs #123"
```

### Task 4: recommendation serviceで提供元を統合する

**Files:**
- Create: `server/internal/recommendation/service.go`, `service_test.go`

**Interfaces:**

```go
type ProviderSet struct { Qiita provider.Client; Zenn provider.Client }
type Service struct {
    Interests InterestRepository; Providers ProviderSet; Clock Clock; FeedTimeout time.Duration
}
type Result struct {
    QiitaArticles []article.Article
    ZennArticles []article.Article
    Warnings []Warning
}
func (s Service) Get(context.Context, int64) (Result, error)
func (s Service) RecordClick(context.Context, int64, []string) error
```

- [ ] **Step 1: 期間・上位タグ・重複統合テストを書く**

興味度上位5件だけで各提供元を検索し、取得後にQiitaはnow-5日、Zennはnow-14日より新しい記事だけを候補にする。同一URLは1件へ統合し、Zennは検索に使ったタグを重複なしでArticle.Tagsへ加えることをfake providerの呼び出し記録で確認する。

- [ ] **Step 2: Zennフォールバックをテストする**

期間内Zennが0件なら同じ取得済み候補から期間条件だけ外して採用する。新しい外部requestは増やさない。Qiitaには同じフォールバックを適用しない。

- [ ] **Step 3: 部分成功とdeadlineをテストする**

Qiita成功/Zenn失敗、逆、両方失敗、親context cancel、FeedTimeout超過を表形式で確認する。片方成功はResultとwarning、両方失敗は `ErrAllProvidersFailed`。エラー文字列へ検索タグを入れない。

- [ ] **Step 4: 実装する**

興味度をDBから取得してからFeedTimeout contextを作り、上位5タグ×2提供元の最大10 requestをgoroutineで取得する。buffer 10のchannelへ各goroutineが必ず1結果を送り、全件回収後にURL統合とScoreArticlesを行う。提供元内で1件でもrequestが失敗すればwarningを付け、1件以上成功すれば空配列でもその提供元は利用可能と判定する。Qiita/Zennをそれぞれ上位10件へ絞り、内部scoreを外したArticleとして返す。`RecordClick` はrepositoryへ委譲する。

- [ ] **Step 5: race付きテストしてコミットする**

```bash
cd server
go test ./internal/recommendation -race -count=20
git add server/internal/recommendation/service.go server/internal/recommendation/service_test.go
git commit -m "feat(feed): 記事統合と部分成功を実装" -m "Refs #123"
```

### Task 5: feedとclickのHTTP契約を公開する

**Files:**
- Create: `server/internal/recommendation/handler.go`, `handler_test.go`
- Modify: `server/internal/app/app.go`, `app_test.go`

**Interfaces:**
- Produces: `GET /api/v1/feed` 200/401/503
- Produces: `POST /api/v1/feedback/article-clicks` 204/401/422

- [ ] **Step 1: 認証境界テストを書く**

Cookieなし/期限切れは401でservice未呼び出し。正しいsessionは認証利用者IDだけをserviceへ渡す。bodyやqueryからuser IDを受け取らない。

- [ ] **Step 2: response契約テストを書く**

feedは `qiita_articles`、`zenn_articles`、`warnings` を返し、各記事はtitle/url/source/tags/likes/published_atを持つ。内部scoreは応答へ出さない。click bodyは `tags` の1〜50件、各1〜50文字を受け、成功204は空body。未知フィールドは422にする。

- [ ] **Step 3: handlerと経路登録を実装する**

auth middlewareがcontextへ入れたUserを取得する。外部両失敗だけ503 Problem Details、部分成功warningは提供元名と分類codeだけを公開し、生エラーを返さない。

- [ ] **Step 4: HTTPテストしてコミットする**

```bash
cd server
go test ./internal/recommendation ./internal/app -race -v
git add server/internal/recommendation/handler.go server/internal/recommendation/handler_test.go server/internal/app
git commit -m "feat(api): feedとクリック学習APIを公開" -m "Refs #123"
```

### Task 6: 実依存とOpenAPIを統合して検証する

**Files:**
- Modify: `server/cmd/api/main.go`, `cmd/openapi/main.go`
- Modify: `server/openapi/openapi.json`

- [ ] **Step 1: repository、provider、recommendation serviceを注入する**

API起動時にauth、store、#122のprovider client、recommendation serviceを1回組み立て、認証と推薦の経路を登録する。feed requestごとにDB pool/http.Clientを作らない。

- [ ] **Step 2: OpenAPIを再生成・差分確認する**

```bash
cd server
go tool sqlc generate
go run ./cmd/openapi
git diff --exit-code -- internal/store/dbgen openapi/openapi.json
```

Expected: feed/click経路、cookieAuth、Article、Warning、Problem Details、全statusが仕様へ含まれ、Article schemaに内部scoreが無い。

- [ ] **Step 3: 全検証を行う**

```bash
cd server
gofmt -w .
go vet ./...
go test ./... -race
go build ./cmd/api ./cmd/migrate ./cmd/openapi
```

- [ ] **Step 4: 統合をコミットする**

```bash
git add server/cmd server/internal/store/dbgen server/openapi/openapi.json
git commit -m "feat(recommend): 推薦機能をGo APIへ統合" -m "Refs #123"
```

- [ ] **Step 5: PRを作る**

PRタイトルは `feat(recommend): 固定小数点の興味学習と記事推薦をGoへ移植する`。本文に丸め差、transaction競合試験、Qiita/Zennのスコア差、Zennフォールバック、部分成功を記載し、`Closes #123` を付ける。人間がレビュー・マージする。
