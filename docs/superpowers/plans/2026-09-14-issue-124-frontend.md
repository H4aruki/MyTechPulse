# Issue 124 Frontend Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** React画面をGo `/api/v1` 契約とHttpOnly Cookie認証へ切り替え、OpenAPI生成型で契約ずれをCI検出する。

**Architecture:** Go生成の `server/openapi/openapi.json` を正本とし、openapi-typescriptで型だけを生成する。既存fetch wrapperをcredentials・CSRF・Problem Details対応へ置き換え、認証状態は `/auth/me` のserver stateとしてReact Queryで管理する。

**Tech Stack:** React 19、TypeScript 6、TanStack Query 5、Vitest 5、openapi-typescript 7.13.0

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- JWTと `localStorage` を新APIで使用しない
- ブラウザに既に残る `access_token` は読まず、明示許可なしに削除もしない。旧JWTは期限切れに任せる
- Cookie値をJavaScriptから読まない
- `fetch` は全API requestで `credentials: 'include'` を指定する
- POST/PUT/PATCH/DELETEは `X-MTP-CSRF: 1` を付ける
- 401だけをログイン画面へ戻す。403/409/422/5xxは意味を失わず表示する
- API request/response型を手書きで複製しない
- 既存画面構成と記事閲覧体験を保ち、管理者画面は作らない
- `openapi-typescript@7.13.0` は承認済み追加依存として正確に固定する
- #125で旧frontend/backendのmain自動公開が凍結済みと確認するまで、このPRをmainへ取り込まない

---

## File Map

- Modify: `frontend/package.json`, `package-lock.json` — 型生成toolとscripts
- Create: `frontend/src/api/generated.ts` — OpenAPIからの生成物
- Create: `frontend/src/api/generated-contract.ts` — TypeScriptビルドによる主要経路の型存在確認（手書き、Vitest対象外）
- Modify: `frontend/src/api/client.ts` — Cookie、CSRF、Problem Details
- Modify: `frontend/src/api/endpoints.ts` — `/api/v1` wrapper
- Modify: `frontend/src/api/types.ts` — 生成型の読みやすいaliasだけ
- Create: `frontend/src/api/client.test.ts`, `endpoints.test.ts` — HTTP契約
- Modify: `frontend/src/lib/auth.ts`, `auth.test.ts` — server session query helper
- Modify: `frontend/src/components/ProtectedRoute.tsx`, `ProtectedRoute.test.tsx` — `/me` 判定
- Modify: `frontend/src/pages/LoginPage.tsx`, `SignupPage.tsx`, `ArticlesPage.tsx` — 新契約
- Create: `frontend/src/pages/LoginPage.test.tsx`, `SignupPage.test.tsx`, `ArticlesPage.test.tsx`
- Modify: `.github/workflows/ci.yml` — 生成差分検査

### Task 1: OpenAPI型生成を固定する

**Files:**
- Modify: `frontend/package.json`, `frontend/package-lock.json`
- Create: `frontend/src/api/generated.ts`, `generated-contract.ts`

**Interfaces:**
- Consumes: `server/openapi/openapi.json`
- Produces: `paths`、`components`、`operations` TypeScript型

- [ ] **Step 1: 承認済みtoolを固定追加する**

```bash
cd frontend
npm install --save-dev --save-exact openapi-typescript@7.13.0
```

Expected: packageとlockの版がどちらも `7.13.0`。

- [ ] **Step 2: scriptsを追加する**

```json
{
  "scripts": {
    "api:generate": "openapi-typescript ../server/openapi/openapi.json -o src/api/generated.ts",
    "api:check": "npm run api:generate && git diff --exit-code -- src/api/generated.ts"
  }
}
```

既存のdev/build/lint/test/previewは残す。型確認のための追加依存やtest scriptの変更は行わない。

- [ ] **Step 3: 主要経路のcompile-time型確認を書く**

次を手書きの `frontend/src/api/generated-contract.ts` に置く。通常の `.ts` ファイル名によりVitestのtest/spec収集対象から外す。`tsconfig.app.json` の `include: ["src"]` に含まれるので、既存の `npm run build`（`tsc -b && vite build`）または `npx tsc -b` が型検査し、経路・methodが生成型から失われると失敗する。画面コードからimportする必要はない。型生成の出力先は `generated.ts` のままとする。

```ts
import type { paths } from './generated'

type Login = paths['/api/v1/auth/login']['post']
type Feed = paths['/api/v1/feed']['get']
type Click = paths['/api/v1/feedback/article-clicks']['post']

export const generatedContractExists: [Login, Feed, Click] | null = null
```

- [ ] **Step 4: 生成物を確認してコミットする**

```bash
cd frontend
npm run api:generate
npm run build
npm run api:check
git add package.json package-lock.json src/api/generated.ts src/api/generated-contract.ts
git commit -m "chore(frontend): OpenAPI型生成を追加" -m "Refs #124"
```

### Task 2: Cookie/CSRF対応の共通clientを実装する

**Files:**
- Modify: `frontend/src/api/client.ts`
- Create: `frontend/src/api/client.test.ts`

**Interfaces:**

```ts
export interface ProblemDetails {
  type: string
  title: string
  status: number
  detail?: string
  code?: string
  errors?: Record<string, string[]>
}
export class UnauthorizedError extends Error {}
export class ApiError extends Error { constructor(readonly problem: ProblemDetails) }
export async function request<T>(method: 'GET' | 'POST', path: string, body?: unknown): Promise<T>
```

- [ ] **Step 1: fetch契約の失敗テストを書く**

GET/POSTの両方にcredentials include、POSTだけにContent-Typeと `X-MTP-CSRF: 1`、Authorizationなしを確認する。204はbodyを読まずundefined、401はUnauthorizedError、Problem DetailsはApiError、不正JSON/通信失敗は安全な日本語ApiErrorにする。

- [ ] **Step 2: 未実装の失敗を確認する**

```bash
cd frontend
npm run test -- src/api/client.test.ts
```

- [ ] **Step 3: 共通clientを実装する**

```ts
const response = await fetch(`${API_BASE_URL}${path}`, {
  method,
  credentials: 'include',
  headers: method === 'GET'
    ? { Accept: 'application/json' }
    : { Accept: 'application/json', 'Content-Type': 'application/json', 'X-MTP-CSRF': '1' },
  body: body === undefined ? undefined : JSON.stringify(body),
})
```

401判定を最初に行い、204ならdecodeせず返す。失敗応答は `application/problem+json` のときだけProblemDetailsとしてdecodeし、HTMLや空bodyを画面へ露出しない。

- [ ] **Step 4: テストしてコミットする**

```bash
cd frontend
npm run test -- src/api/client.test.ts
git add src/api/client.ts src/api/client.test.ts
git commit -m "feat(frontend): Cookie認証のAPI clientへ切替" -m "Refs #124"
```

### Task 3: 生成型でendpoint wrapperを実装する

**Files:**
- Modify: `frontend/src/api/endpoints.ts`, `types.ts`
- Create: `frontend/src/api/endpoints.test.ts`

**Interfaces:**

```ts
type Schemas = components['schemas']
export type User = Schemas['User']
export type Article = Schemas['Article']
export type FeedResponse = Schemas['FeedResponse']

export function login(body: Schemas['LoginRequest']): Promise<Schemas['AuthResponse']>
export function signup(body: Schemas['SignupRequest']): Promise<Schemas['AuthResponse']>
export function currentUser(): Promise<User>
export function logout(): Promise<void>
export function fetchFeed(): Promise<FeedResponse>
export function recordArticleClick(tags: string[]): Promise<void>
```

- [ ] **Step 1: path/method/body testを書く**

mock fetchでlogin、signup、me、logout、feed、clickの正確な `/api/v1` pathとmethodを検査する。signup bodyは `username/password/favorite_tags`、click bodyは `{ tags }` とする。

- [ ] **Step 2: aliasとwrapperを実装する**

`types.ts` はgenerated componentsのaliasと画面専用型だけをexportし、旧 `ApiStatus`、access_token、手書きArticleは削除する。これは既存ファイル内のコード編集で、ファイル自体は削除しない。

- [ ] **Step 3: テストしてコミットする**

```bash
cd frontend
npm run test -- src/api/endpoints.test.ts
npm run build
git add src/api/endpoints.ts src/api/types.ts src/api/endpoints.test.ts
git commit -m "feat(frontend): Go APIの生成型へ接続" -m "Refs #124"
```

### Task 4: 認証状態を `/auth/me` へ切り替える

**Files:**
- Modify: `frontend/src/lib/auth.ts`, `auth.test.ts`
- Modify: `frontend/src/components/ProtectedRoute.tsx`, `ProtectedRoute.test.tsx`

**Interfaces:**

```ts
export const authQueryKey = ['auth', 'me'] as const
export function useCurrentUser(): UseQueryResult<User, Error>
```

- [ ] **Step 1: 3状態のroute testを書く**

`/me` pendingは読み込み表示、successはchildren、401は `/login` へreplace、通信/5xxは再試行可能なerror表示とする。localStorageに `access_token` があっても認証済み扱いしない。

- [ ] **Step 2: tokenStorageをserver state helperへ置き換える**

`useCurrentUser` は `currentUser`、retry false、staleTime 5分を使う。login/signup成功時はQueryClientへUserをset、logout成功時はauth/feed queryをremoveする。

- [ ] **Step 3: テストしてコミットする**

```bash
cd frontend
npm run test -- src/lib/auth.test.ts src/components/ProtectedRoute.test.tsx
git add src/lib/auth.ts src/lib/auth.test.ts src/components/ProtectedRoute.tsx src/components/ProtectedRoute.test.tsx
git commit -m "feat(frontend): 認証判定をserver sessionへ変更" -m "Refs #124"
```

### Task 5: ログイン・登録・記事画面を新契約へ接続する

**Files:**
- Modify: `frontend/src/pages/LoginPage.tsx`, `SignupPage.tsx`, `ArticlesPage.tsx`
- Create: `frontend/src/pages/LoginPage.test.tsx`, `SignupPage.test.tsx`, `ArticlesPage.test.tsx`

- [ ] **Step 1: 画面契約テストを書く**

Loginは200で遷移、401で列挙不能な同一文言。Signupは201で遷移、409で同名文言、422で入力文言。Articlesは `qiita_articles` と `zenn_articles` を既存sectionへ表示し、warningを非致命表示、401でloginへ遷移、logout完了後に遷移する。

- [ ] **Step 2: LoginPageをHTTP status契約へ変更する**

`ApiStatus` とtokenStorage処理を外す。mutationは `retry: false`。成功AuthResponseのUserをauth queryへ入れて `/articles` へ遷移する。401以外はApiErrorの安全なdetailを表示する。

- [ ] **Step 3: SignupPageを新field名へ変更する**

フォーム内部名を `username/password`、requestを `{ username, password, favorite_tags: [...selectedTags] }` とする。username 1〜50文字、passwordは `new TextEncoder().encode(value).length` で1〜72byte、favorite tagsは1〜128件・各1〜50文字を画面でも検証する。

- [ ] **Step 4: ArticlesPageをfeed/logout契約へ変更する**

`fetchPersonalNews` を `fetchFeed` に変更し、既存の `source` 表示を維持する。click mutationは `retry: false`。logoutはserverへPOSTし、失敗時も401ならlocal queryを消してloginへ移す。その他の失敗は再試行表示し、server sessionが残る可能性を隠さない。

- [ ] **Step 5: UIテストしてコミットする**

```bash
cd frontend
npm run test -- src/pages
git add src/pages
git commit -m "feat(frontend): 主要画面をGo API契約へ移行" -m "Refs #124"
```

### Task 6: CIで生成ずれと回帰を止める

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: frontend jobへ生成検査を追加する**

```yaml
- name: OpenAPI生成型のずれを確認
  run: npm run api:check
```

working-directoryは既存job既定の `frontend` を使う。既存必須job名は変更しない。

- [ ] **Step 2: 全フロント検証を行う**

```bash
cd frontend
npm run api:check
npm run lint
npm run test
npm run build
```

- [ ] **Step 3: ブラウザ確認を行う**

localのGo APIとfrontendを起動し、登録→記事表示→記事クリック→再読込→logout→保護画面からloginへの流れを合成利用者で確認する。DevToolsでCookie本文は記録せず、requestにcredentialsとCSRF headerがあることだけを記録する。

- [ ] **Step 4: CI変更をコミットする**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(frontend): OpenAPI生成ずれを検出" -m "Refs #124"
```

- [ ] **Step 5: PRを作る**

PRタイトルは `feat(frontend): Go APIとHttpOnly Cookie認証へ接続する`。本文に依存追加、生成型、401/409/422、CORS/CSRF、主要画面の確認結果を記載し、`Closes #124` を付ける。人間がレビュー・マージする。
