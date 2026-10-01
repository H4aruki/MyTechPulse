# Issue 120 Go Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 業務機能を持たないGo API共通基盤を `server/` に構築し、設定、DB接続、ログ、エラー、health、OpenAPI、Swagger UI、Docker起動を検証できるようにする。

**Architecture:** 標準 `net/http` のServeMuxにHuma v2を接続し、`internal/app` が依存を組み立てる。業務モジュールはHuma・pgxへ直接依存せず、設定と技術アダプターを `internal/platform` に閉じ込める。

**Execution order:** #119のスキーマ監査・バックアップ準備後に実施する。本IssueはSQLマイグレーション0件の状態でcompile/test・Docker検証まで完了でき、ベースラインSQLには依存しない。完了後に#119後半が同じmigrationディレクトリへベースラインを追加し、復元検証を完成する。

**Tech Stack:** Go 1.26、Huma v2.39.1、pgx v5.11.0、goose v3.28.0、sqlc v1.31.1、PostgreSQL 17

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- 承認済み依存だけを指定バージョンで追加する
- Go module pathは `github.com/H4aruki/MyTechPulse/server` とする
- 追加HTTPルーター、ORM、DIコンテナ、汎用リトライライブラリを加えない
- `.env` と `backend/.env` を読まず、`server/.env.example` だけを作る
- 本番の秘密値に開発用既定値を置かない
- Go版はローカルで `127.0.0.1:8001` を使い、Python版 `127.0.0.1:8000` と併存する
- ComposeのGo serviceは `go-preview` profileへ隔離し、現行の引数なしproduction起動へ混ぜない
- Swagger UIはlocal/testで有効、productionでは関連経路をすべて無効にする
- API起動時にDBマイグレーションを実行しない

---

## File Map

- Create: `server/go.mod`, `server/go.sum` — moduleと固定依存
- Create: `server/sqlc.yaml` — SQL生成設定
- Create: `server/cmd/api/main.go` — API起動と正常終了
- Create: `server/cmd/migrate/main.go` — 明示マイグレーション
- Create: `server/cmd/openapi/main.go` — DB接続なしの仕様生成
- Create: `server/internal/app/app.go` — Humaと経路の組み立て
- Create: `server/internal/platform/config/config.go`, `config_test.go` — 設定と検証
- Create: `server/internal/platform/logging/logging.go`, `logging_test.go` — `slog` JSON設定
- Create: `server/internal/platform/httpx/middleware.go`, `middleware_test.go` — request ID、recover、秘密値を出さないアクセスログ
- Create: `server/internal/platform/httpx/problem.go`, `problem_test.go` — Problem Details
- Create: `server/internal/platform/postgres/pool.go`, `pool_test.go` — pgxpool生成
- Create: `server/internal/health/handler.go`, `handler_test.go` — live/ready
- Create: `server/internal/migrate/run.go`, `run_test.go` — SQL0件対応の埋め込みgoose実行
- Create: `server/db/migrations/embed.go`, `README.md` — migration FSと常設marker
- Create: `server/openapi/openapi.json` — 生成仕様
- Create: `server/.env.example`, `server/Dockerfile`, `server/.dockerignore`
- Modify: `docker-compose.yml` — `api-go` を並行追加
- Modify: `.gitignore`, `.dockerignore` — Go生成物とビルドコンテキスト

### Task 1: Go moduleと承認済み依存を固定する

**Files:**
- Create: `server/go.mod`
- Create: `server/go.sum`
- Create: `server/sqlc.yaml`

**Interfaces:**
- Consumes: Go 1.26 toolchain
- Produces: module `github.com/H4aruki/MyTechPulse/server` と `go tool sqlc`

- [ ] **Step 1: moduleを作る**

```bash
mkdir server
cd server
go mod init github.com/H4aruki/MyTechPulse/server
```

- [ ] **Step 2: 承認済み依存を固定追加する**

```bash
go get github.com/danielgtaylor/huma/v2@v2.39.1
go get github.com/jackc/pgx/v5@v5.11.0
go get github.com/pressly/goose/v3@v3.28.0
go get golang.org/x/crypto@v0.57.0
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go mod tidy
```

Expected: `go.mod` の `go` は1.26、tool directiveにsqlc v1.31.1、runtime依存に指定版が記録される。

- [ ] **Step 3: sqlc設定を書く**

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: db/migrations
    queries: db/queries
    gen:
      go:
        package: dbgen
        out: internal/store/dbgen
        sql_package: pgx/v5
        emit_interface: true
        emit_empty_slices: true
        rename:
          user_ID: UserID
          tag_ID: TagID
        overrides: []
```

- [ ] **Step 4: toolchainを確認する**

```bash
go version
go tool sqlc version
go list -m all
```

Expected: Go 1.26.x、sqlc v1.31.1、指定したmodule版。

- [ ] **Step 5: moduleをコミットする**

```bash
git add server/go.mod server/go.sum server/sqlc.yaml
git commit -m "chore(backend): Goモジュールを初期化" -m "Refs #120"
```

### Task 2: 設定を起動前に検証する

**Files:**
- Create: `server/internal/platform/config/config.go`
- Create: `server/internal/platform/config/config_test.go`
- Create: `server/.env.example`

**Interfaces:**
- Produces: `config.Load(lookup func(string) (string, bool)) (config.Config, error)`
- Produces: `Config{Environment, HTTPAddr, DatabaseURL, QiitaToken, CORSOrigins, SessionTTL, ProviderTimeout, FeedTimeout, ProviderMaxBytes, SwaggerEnabled, CookieName, CookieSecure}`

- [ ] **Step 1: 失敗テストを書く**

```go
func TestLoadRejectsMissingDatabaseURL(t *testing.T) {
    _, err := Load(mapLookup(map[string]string{"APP_ENV": "local"}))
    if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
        t.Fatalf("expected DATABASE_URL error, got %v", err)
    }
}

func TestLoadRejectsSwaggerInProduction(t *testing.T) {
    values := validValues()
    values["APP_ENV"] = "production"
    values["SWAGGER_ENABLED"] = "true"
    _, err := Load(mapLookup(values))
    if err == nil || !strings.Contains(err.Error(), "SWAGGER_ENABLED") {
        t.Fatalf("expected SWAGGER_ENABLED error, got %v", err)
    }
}
```

テスト補助に外部assertライブラリは追加せず、標準 `testing` で判定する。

- [ ] **Step 2: テストが未実装で失敗することを確認する**

```bash
go test ./internal/platform/config -v
```

Expected: `Load` undefinedでFAIL。

- [ ] **Step 3: 最小設定ローダーを実装する**

```go
type Config struct {
    Environment      string
    HTTPAddr         string
    DatabaseURL      string
    QiitaToken       string
    CORSOrigins      []string
    SessionTTL       time.Duration
    ProviderTimeout  time.Duration
    FeedTimeout      time.Duration
    ProviderMaxBytes int64
    SwaggerEnabled   bool
    CookieName       string
    CookieSecure     bool
}

func Load(lookup func(string) (string, bool)) (Config, error) {
    environment := getDefault(lookup, "APP_ENV", "local")
    if environment != "local" && environment != "test" && environment != "production" {
        return Config{}, errors.New("APP_ENVはlocal/test/productionのいずれかにしてください")
    }
    databaseURL, err := required(lookup, "DATABASE_URL")
    if err != nil { return Config{}, err }
    qiitaToken, err := required(lookup, "QIITA_ACCESS_TOKEN")
    if err != nil { return Config{}, err }
    origins, err := parseOrigins(getDefault(lookup, "CORS_ALLOWED_ORIGINS", "http://localhost:5173"))
    if err != nil { return Config{}, err }
    sessionTTL, err := parseDuration(lookup, "SESSION_TTL", "24h")
    if err != nil { return Config{}, err }
    providerTimeout, err := parseDuration(lookup, "PROVIDER_TIMEOUT", "5s")
    if err != nil { return Config{}, err }
    feedTimeout, err := parseDuration(lookup, "FEED_TIMEOUT", "8s")
    if err != nil { return Config{}, err }
    maxBytes, err := parsePositiveInt64(lookup, "PROVIDER_MAX_BYTES", "2097152")
    if err != nil { return Config{}, err }
    swagger, err := parseBool(lookup, "SWAGGER_ENABLED", environment != "production")
    if err != nil { return Config{}, err }
    if environment == "production" && swagger {
        return Config{}, errors.New("productionではSWAGGER_ENABLEDをtrueにできません")
    }
    cookieName, cookieSecure := "mtp_session", false
    if environment == "production" {
        cookieName, cookieSecure = "__Host-mtp_session", true
    }
    return Config{
        Environment: environment,
        HTTPAddr: getDefault(lookup, "HTTP_ADDR", ":8001"),
        DatabaseURL: databaseURL,
        QiitaToken: qiitaToken,
        CORSOrigins: origins,
        SessionTTL: sessionTTL,
        ProviderTimeout: providerTimeout,
        FeedTimeout: feedTimeout,
        ProviderMaxBytes: maxBytes,
        SwaggerEnabled: swagger,
        CookieName: cookieName,
        CookieSecure: cookieSecure,
    }, nil
}
```

`required` はtrim後空ならkey名だけを含むerror、`getDefault` は未設定だけ既定値、`parseDuration` は0より大きい `time.ParseDuration`、`parsePositiveInt64` は10進の正整数、`parseBool` は `strconv.ParseBool` を使う。`parseOrigins` はcomma分割後、`http`/`https`、hostあり、path/query/fragmentなしのoriginだけを許可し、`*` とuserinfoを拒否する。全helperのerrorへ入力値を含めない。

- [ ] **Step 4: `.env.example` に名前と安全な例だけを書く**

```dotenv
APP_ENV=local
HTTP_ADDR=:8001
DATABASE_URL=postgresql://postgres:postgres@127.0.0.1:5432/mytechpulse
QIITA_ACCESS_TOKEN=replace-with-local-token
CORS_ALLOWED_ORIGINS=http://localhost:5173
SESSION_TTL=24h
PROVIDER_TIMEOUT=5s
FEED_TIMEOUT=8s
PROVIDER_MAX_BYTES=2097152
SWAGGER_ENABLED=true
```

- [ ] **Step 5: テストとコミットを行う**

```bash
go test ./internal/platform/config -v
git add server/internal/platform/config server/.env.example
git commit -m "feat(backend): Go設定を起動時検証" -m "Refs #120"
```

### Task 3: 構造化ログとHTTP共通処理を作る

**Files:**
- Create: `server/internal/platform/logging/logging.go`
- Create: `server/internal/platform/logging/logging_test.go`
- Create: `server/internal/platform/httpx/middleware.go`
- Create: `server/internal/platform/httpx/middleware_test.go`
- Create: `server/internal/platform/httpx/problem.go`
- Create: `server/internal/platform/httpx/problem_test.go`

**Interfaces:**
- Produces: `logging.New(w io.Writer, level slog.Level) *slog.Logger`
- Produces: `httpx.RequestID(next http.Handler) http.Handler`
- Produces: `httpx.Recover(logger *slog.Logger, next http.Handler) http.Handler`
- Produces: `httpx.AccessLog(logger *slog.Logger, next http.Handler) http.Handler`
- Produces: `httpx.Problem{Type, Title, Status, Detail, Instance, Code, Errors}`

- [ ] **Step 1: request IDと秘密値非出力のテストを書く**

```go
func TestAccessLogDoesNotLogHeadersOrBody(t *testing.T) {
    var logs bytes.Buffer
    logger := logging.New(&logs, slog.LevelInfo)
    handler := AccessLog(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusNoContent)
    }))
    req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"password":"secret"}`))
    req.Header.Set("Cookie", "__Host-mtp_session=secret-session")
    handler.ServeHTTP(httptest.NewRecorder(), req)
    if strings.Contains(logs.String(), "secret") {
        t.Fatalf("secret leaked: %s", logs.String())
    }
}
```

- [ ] **Step 2: panic時の一般エラーをテストする**

```go
func TestRecoverReturnsGenericProblem(t *testing.T) {
    handler := Recover(slog.New(slog.NewJSONHandler(io.Discard, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
        panic("database password must not escape")
    }))
    recorder := httptest.NewRecorder()
    handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))
    if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "password") {
        t.Fatalf("unsafe response: %d %s", recorder.Code, recorder.Body.String())
    }
}
```

- [ ] **Step 3: 最小実装を追加する**

`crypto/rand` から128bitのrequest IDを作り、既存の妥当な `X-Request-ID` があれば引き継ぐ。アクセスログはmethod、path、status、duration、request_idだけを記録する。Problem Detailsは `application/problem+json` を使う。

- [ ] **Step 4: race付きテストを実行する**

```bash
go test ./internal/platform/logging ./internal/platform/httpx -race -v
```

Expected: all pass、テストログに `secret` が無い。

- [ ] **Step 5: 共通HTTP処理をコミットする**

```bash
git add server/internal/platform/logging server/internal/platform/httpx
git commit -m "feat(backend): HTTP共通処理と安全なログを追加" -m "Refs #120"
```

### Task 4: DB接続と明示マイグレーション基盤を作る

**Files:**
- Create: `server/internal/platform/postgres/pool.go`
- Create: `server/internal/platform/postgres/pool_test.go`
- Create: `server/internal/migrate/run.go`
- Create: `server/internal/migrate/run_test.go`
- Create: `server/db/migrations/embed.go`
- Create: `server/db/migrations/README.md`
- Create: `server/cmd/migrate/main.go`

**Interfaces:**
- Produces: `postgres.Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)`
- Produces: `migrate.Run(ctx context.Context, db *sql.DB, migrationFS fs.FS) error`

- [ ] **Step 1: 接続文字列をエラーへ含めないテストを書く**

```go
func TestOpenDoesNotReturnDatabaseURL(t *testing.T) {
    raw := "postgresql://secret-user:secret-pass@127.0.0.1:1/missing"
    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()
    _, err := Open(ctx, raw)
    if err == nil || strings.Contains(err.Error(), "secret-pass") {
        t.Fatalf("unsafe error: %v", err)
    }
}
```

- [ ] **Step 2: pgxpoolを作りPingする実装を書く**

```go
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
    cfg, err := pgxpool.ParseConfig(databaseURL)
    if err != nil {
        return nil, errors.New("DATABASE_URLの形式が不正です")
    }
    pool, err := pgxpool.NewWithConfig(ctx, cfg)
    if err != nil {
        return nil, errors.New("DB接続を作成できません")
    }
    if err := pool.Ping(ctx); err != nil {
        pool.Close()
        return nil, errors.New("DBへ接続できません")
    }
    return pool, nil
}
```

- [ ] **Step 3: goose runnerと埋め込みFSを実装する**

```go
package migrations

import "embed"

//go:embed *
var FS embed.FS
```

常設 `server/db/migrations/README.md` の本文は「このディレクトリにはgoose SQLを追加する。SQLが0件でも埋め込みを成立させるため、このファイルを保持する。最初のSQLは#119で追加する。」とする。単一pattern `*` がREADMEと後続SQLを含み、SQL0件でも一致する。Goソース等もFSに含まれるが、runner/gooseが処理対象にするのは直下のSQLだけとする。#119は既定パスに `00001_legacy_baseline.sql` を追加するだけでよく、ディレクトリやsqlcのschema設定を変えない。

`migrate.Run` の冒頭で次を実行し、SQLが0件ならDBへ触れず成功する。SQLがある場合のみ `goose.SetBaseFS(migrationFS)`、dialect `postgres`、`goose.UpContext(ctx, db, ".")` を呼ぶ。標準 `database/sql` 接続を用意する処理は `cmd/migrate` へ集約し、API `main.go` からは呼ばない。

```go
files, err := fs.Glob(migrationFS, "*.sql")
if err != nil {
    return err
}
if len(files) == 0 {
    return nil
}
```

`run_test.go` には次を追加する。DB結合テストと異なり、SQL0件のテストはDB設定なしでも必ず実行する。

```go
func TestRunWithoutSQLDoesNotAccessDatabase(t *testing.T) {
    markerOnly := fstest.MapFS{
        "README.md": &fstest.MapFile{Data: []byte("migration marker")},
    }
    if err := Run(context.Background(), nil, markerOnly); err != nil {
        t.Fatalf("empty migrations: %v", err)
    }
}
```

`cmd/migrate` は標準 `flag` で `--help` をDB接続・設定読込より先に処理し、使い方を表示してexit 0とする。通常実行は `DATABASE_URL` を検証し、SQL0件も正常終了する。

- [ ] **Step 4: テストする**

```bash
go test ./internal/platform/postgres ./internal/migrate -race -v
go test ./db/migrations
go build ./cmd/migrate
```

Expected: SQLファイルを追加していない状態でcompile/test成功、SQL0件とURL非漏えいテストpass。DB結合テストは `TEST_DATABASE_URL` 未設定時skip。

- [ ] **Step 5: DB基盤をコミットする**

```bash
git add server/internal/platform/postgres server/internal/migrate server/db/migrations/embed.go server/db/migrations/README.md server/cmd/migrate/main.go
git commit -m "feat(db): Goの接続と明示マイグレーション基盤を追加" -m "Refs #120"
```

### Task 5: health、OpenAPI、Swagger UIを構築する

**Files:**
- Create: `server/internal/health/handler.go`
- Create: `server/internal/health/handler_test.go`
- Create: `server/internal/app/app.go`
- Create: `server/internal/app/app_test.go`
- Create: `server/cmd/openapi/main.go`
- Create: `server/openapi/openapi.json`

**Interfaces:**
- Produces: `health.ReadyChecker{Ping(context.Context) error}`
- Produces: `health.Register(api huma.API, checker ReadyChecker)`
- Produces: `app.New(cfg config.Config, deps app.Dependencies) (http.Handler, *huma.OpenAPI)`
- Produces: live/ready成功body `{ "status": "ok" }`、ready失敗は503 Problem Details

- [ ] **Step 1: live/readyテストを書く**

```go
func TestReadyReturns503WhenDatabaseFails(t *testing.T) {
    handler, _ := app.New(testConfig(), app.Dependencies{Ready: failingChecker{}})
    response := httptest.NewRecorder()
    handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
    if response.Code != http.StatusServiceUnavailable {
        t.Fatalf("got %d", response.Code)
    }
}
```

- [ ] **Step 2: Huma設定と経路登録を実装する**

```go
func New(cfg config.Config, deps Dependencies) (http.Handler, *huma.OpenAPI) {
    mux := http.NewServeMux()
    hc := huma.DefaultConfig("MyTechPulse API", "1.0.0")
    hc.DocsRenderer = huma.DocsRendererSwaggerUI
    hc.DocsPath = "/docs"
    hc.OpenAPIPath = "/openapi" // Humaが.jsonと.yamlを付けて公開する基底パス
    if !cfg.SwaggerEnabled {
        hc.DocsPath, hc.OpenAPIPath, hc.SchemasPath = "", "", ""
    }
    api := humago.New(mux, hc)
    health.Register(api, deps.Ready)
    return httpx.RequestID(httpx.Recover(deps.Logger, httpx.AccessLog(deps.Logger, mux))), api.OpenAPI()
}
```

- [ ] **Step 3: DB不要のOpenAPI生成コマンドを書く**

`cmd/openapi` はno-op `ReadyChecker` と破棄loggerで `app.New` を呼び、返されたspecを `json.MarshalIndent` して `openapi/openapi.json` へ書く。API起動やDB接続は行わない。

- [ ] **Step 4: 仕様と公開範囲を検証する**

```bash
go run ./cmd/openapi
go test ./internal/health ./internal/app -v
git add openapi/openapi.json
go run ./cmd/openapi
git diff --exit-code -- openapi/openapi.json
```

Expected: local/test設定では `/docs`、`/openapi.json`、`/openapi.yaml`、`/openapi-3.0.json`、`/openapi-3.0.yaml`、登録済みschemaの `/schemas/ReadyOutputBody.json` がすべて200。production相当ではこれら各経路がすべて404。liveは常に200、DB失敗readyは503。

- [ ] **Step 5: API仕様基盤をコミットする**

```bash
git add server/internal/health server/internal/app server/cmd/openapi server/openapi/openapi.json
git commit -m "feat(api): healthとSwagger UIを追加" -m "Refs #120"
```

### Task 6: 正常終了とDocker並行起動を追加する

**Files:**
- Create: `server/cmd/api/main.go`
- Create: `server/Dockerfile`
- Create: `server/.dockerignore`
- Modify: `docker-compose.yml`
- Modify: `.gitignore`
- Modify: `.dockerignore`

**Interfaces:**
- Consumes: `app.New`、`postgres.Open`
- Produces: Go API `127.0.0.1:8001`、graceful shutdown 10秒

- [ ] **Step 1: mainの組み立て関数をテスト可能に分ける**

```go
func run(ctx context.Context, lookup func(string) (string, bool), stdout, stderr io.Writer) error {
    cfg, err := config.Load(lookup)
    if err != nil { return err }
    logger := logging.New(stdout, slog.LevelInfo)
    pool, err := postgres.Open(ctx, cfg.DatabaseURL)
    if err != nil { return err }
    defer pool.Close()
    handler, _ := app.New(cfg, app.Dependencies{Logger: logger, Ready: pool})
    server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
    return serveUntilCanceled(ctx, server, 10*time.Second)
}
```

- [ ] **Step 2: キャンセル時にShutdownするテストを書く**

listenerを `127.0.0.1:0` で作り、cancel後に新規接続が拒否され、`run` が10秒以内にnilで返ることを確認する。固定portは使わない。

- [ ] **Step 3: multi-stage Dockerfileを書く**

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/api /api
COPY --from=build /out/migrate /migrate
EXPOSE 8001
USER 65532:65532
ENTRYPOINT ["/api"]
```

- [ ] **Step 4: Composeへ並行サービスを追加する**

```yaml
api-go:
  profiles: ["go-preview"]
  build:
    context: .
    dockerfile: server/Dockerfile
  environment:
    APP_ENV: ${APP_ENV:-local}
    DATABASE_URL: postgresql://postgres:${POSTGRES_PASSWORD:-postgres}@db:5432/mytechpulse
    QIITA_ACCESS_TOKEN: ${QIITA_ACCESS_TOKEN:-}
    CORS_ALLOWED_ORIGINS: ${CORS_ALLOWED_ORIGINS:-http://localhost:5173}
    SWAGGER_ENABLED: ${SWAGGER_ENABLED:-true}
  ports:
    - "127.0.0.1:8001:8001"
  depends_on:
    db:
      condition: service_healthy
```

既存 `api` は変更せず、Caddyの接続先もまだ切り替えない。Go版のlocal起動は `docker compose --env-file server/.env --profile go-preview up -d api-go` とし、現行の `docker compose up -d --build` ではGo版を起動しない。disabled profileのために存在しない `server/.env` をbase Composeから直接参照しない。

- [ ] **Step 5: 全検証を実行する**

```bash
cd server
gofmt -w .
go vet ./...
go test ./... -race
go build ./cmd/api ./cmd/migrate ./cmd/openapi
go run ./cmd/openapi
git diff --exit-code -- openapi/openapi.json
cd ..
docker compose config
docker build -t mytechpulse-go:issue120 -f server/Dockerfile .
docker run --rm --entrypoint /migrate mytechpulse-go:issue120 --help
```

Expected: SQLが0件でもall exit 0。最終image内の `/migrate --help` がDB設定なし・非rootで起動し、使い方を表示することを確認する。APIの既定entrypointは `/api` とする。#125のrelease imageでも同じ検証を行い、#119追加後の実際のDB適用は空DB・既存相当DBで別途検証する。Docker daemonが利用不能なら、他の検査結果を記録してDockerだけ未検証と明記し、完了扱いにしない。

- [ ] **Step 6: Docker基盤をコミットする**

```bash
git add server/cmd/api server/Dockerfile server/.dockerignore docker-compose.yml .gitignore .dockerignore
git commit -m "feat(backend): Go APIを並行起動可能にする" -m "Refs #120"
```

- [ ] **Step 7: PRを作る**

PRタイトルは `feat(backend): Goバックエンドの共通基盤とSwagger UIを構築する` とし、追加した依存と固定版、local/productionのSwagger結果、DB失敗、正常終了、Docker結果を記載する。人間のレビュー・マージで `Closes #120` とする。
