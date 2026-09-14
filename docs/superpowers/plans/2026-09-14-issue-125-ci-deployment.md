# Issue 125 CI and Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go・Python互換・frontend・OpenAPI・DockerをPRで検証し、mainの同一commitから作ったimmutable imageだけを承認済み本番環境へ反映する。

**Architecture:** PRではread-only権限で全検査とimage buildを行う。mainでは別release workflowがGHCRへcommit SHA tagでpushしてdigestを成果物にする。このIssueでは本番へ反映せず、#127がGitHub Environment承認後に同じdigestを本番へ渡す。

**Tech Stack:** GitHub Actions、Go 1.26、Node.js 22、Python 3.12、Docker Buildx、GHCR、Docker Compose

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- mainへ直接pushせず、人間がレビュー済みPRをマージする
- PR workflowにwrite権限や本番secretを渡さない
- actionは40桁commit SHAへ固定し、版tagはコメントとして残す
- release imageは `ghcr.io/h4aruki/mytechpulse-api-go` のdigestで本番指定する
- 本番サーバー上で `docker compose build`、`go build`、`git reset` を実行しない
- 本番反映は #127 でGitHub Environment `production` の承認後だけ行う
- image/packageの公開範囲、保存費用、認証用secretの登録はオーナー承認前に変更しない
- container、image、volume、backupを自動削除しない
- Codexは本番サーバーへ直接SSH接続しない
- #124をmainへ取り込む前に、現行frontendとPython backendのmain自動公開を凍結する

---

## File Map

- Modify: `.github/workflows/ci.yml` — Go、生成差分、Docker build検査
- Create: `.github/workflows/release.yml` — 本番反映を含まないimage push
- Create: `scripts/verify-actions-pinned.mjs`, `verify-actions-pinned.test.mjs` — action固定検査
- Create: `ops/deploy_release.sh`, `ops/deploy_release_test.sh` — digest限定反映
- Modify: `docker-compose.yml` — local buildとdigest imageを両立
- Create: `docs/deploy/ci-and-release.md` — 必須check、secret、rollback入力

### Task 1: workflow actionのimmutable pinを検査する

**Files:**
- Create: `scripts/verify-actions-pinned.mjs`, `verify-actions-pinned.test.mjs`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: 未固定actionを検出するテストを書く**

一時YAMLの `uses: actions/checkout@v4` は失敗、`uses: actions/checkout@` + 40桁小文字hex + ` # v4` は成功、local action `uses: ./path` は対象外とする。

- [ ] **Step 2: scannerを実装する**

Node標準APIだけで `.github/workflows/*.yml` を読み、外部 `uses:` のrefが `/^[0-9a-f]{40}$/` でなければfile:lineとaction名を出してexit 1にする。secret値やファイル本文全体は出力しない。

- [ ] **Step 3: 公式repositoryから現在のmajor tag commitを取得する**

```bash
gh api repos/actions/checkout/git/ref/tags/v4 --jq .object.sha
gh api repos/actions/setup-python/git/ref/tags/v5 --jq .object.sha
gh api repos/actions/setup-node/git/ref/tags/v4 --jq .object.sha
gh api repos/actions/setup-go/git/ref/tags/v6 --jq .object.sha
gh api repos/docker/setup-buildx-action/git/ref/tags/v3 --jq .object.sha
gh api repos/docker/login-action/git/ref/tags/v3 --jq .object.sha
gh api repos/docker/build-push-action/git/ref/tags/v6 --jq .object.sha
gh api repos/cloudflare/wrangler-action/git/ref/tags/v3 --jq .object.sha
```

annotated tagの場合は返されたobjectをもう一度 `gh api repos/OWNER/REPO/git/tags/OBJECT_SHA --jq .object.sha` でcommitまで解決する。各 `uses:` を解決した40桁SHAへ置き換え、末尾にmajor tagコメントを付ける。

- [ ] **Step 4: scannerを実行してコミットする**

```bash
node --test scripts/verify-actions-pinned.test.mjs
node scripts/verify-actions-pinned.mjs
git add scripts/verify-actions-pinned.mjs scripts/verify-actions-pinned.test.mjs .github/workflows/ci.yml
git commit -m "ci(security): GitHub Actionをcommitへ固定" -m "Refs #125"
```

### Task 2: PR用Go・契約・image検査を追加する

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Go必須jobを追加する**

job id `go-check`、表示名 `Goバックエンドの検査` とし、checkout→setup-go 1.26 cache→次を実行する。

```yaml
- name: 形式・静的解析・テスト・build
  working-directory: server
  run: |
    test -z "$(gofmt -l .)"
    go vet ./...
    go test ./... -race
    go build ./cmd/api ./cmd/migrate ./cmd/openapi
```

DB結合testにはservice container `postgres:17` と合成 `TEST_DATABASE_URL` をjob envで渡す。本番値は使わない。

- [ ] **Step 2: OpenAPI/sqlc生成差分jobを追加する**

```yaml
- name: sqlcとOpenAPI生成差分
  working-directory: server
  run: |
    go tool sqlc generate
    go run ./cmd/openapi
    git diff --exit-code -- internal/store/dbgen openapi/openapi.json
```

frontend jobでは `npm run api:check` をlint前に実行する。

- [ ] **Step 3: PRのDocker buildを追加する**

Buildxで `server/Dockerfile` を `push: false`、cacheなしでもbuildする。secretとproduction設定は渡さない。

- [ ] **Step 4: 旧frontend/backendの自動公開を凍結する**

既存 `deploy-frontend` と `deploy-backend` の条件へ `vars.LEGACY_DEPLOY_ENABLED == 'true'` を追加し、このrepository variableは作成しない。未設定がfalseとなるworkflow testを行い、mainでも両jobがskipされることを確認する。#124はこの凍結PRがmainへ入った後だけmerge可能とする。移行完了までは既存Python検査を残し、必須checkの表示名を変更しない。

- [ ] **Step 5: ローカル構文と差分を確認してコミットする**

```bash
node scripts/verify-actions-pinned.mjs
docker compose config
git diff --check
git diff -- .github/workflows/ci.yml
git add .github/workflows/ci.yml
git commit -m "ci(backend): Goと生成契約の検査を追加" -m "Refs #125"
```

### Task 3: digest限定の反映scriptをTDDで作る

**Files:**
- Create: `ops/deploy_release.sh`, `ops/deploy_release_test.sh`
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: `GO_API_IMAGE=ghcr.io/h4aruki/mytechpulse-api-go@sha256:` + 64桁hex
- Produces: `api-go` pull、migrate、起動、ready確認。source buildなし

- [ ] **Step 1: fake docker/git/curlで拒否条件を書く**

未設定、tagだけ、別repository、短いdigestはexit 2。正しいdigestは `git pull --ff-only`、`docker compose pull api-go`、one-shot migrate、`docker compose up -d --no-build api-go caddy`、ready確認の順とする。`docker compose build` とpruneが一度も呼ばれないことを検査する。

- [ ] **Step 2: composeのimage指定を追加する**

```yaml
api-go:
  image: ${GO_API_IMAGE:-mytechpulse-api-go:local}
  build:
    context: .
    dockerfile: server/Dockerfile
```

localはbuild可能、本番は環境変数のdigestをpullする。

- [ ] **Step 3: 反映scriptを実装する**

`set -euo pipefail`、完全一致regex、`git pull --ff-only`、pull、migration、起動、最大10回のready確認を実装する。失敗時は終了し、旧containerやimageを削除しない。ログにはdigestとhealth statusだけを出す。

- [ ] **Step 4: shellとcomposeを検証してコミットする**

```bash
bash -n ops/deploy_release.sh ops/deploy_release_test.sh
bash ops/deploy_release_test.sh
docker compose config
git add ops/deploy_release.sh ops/deploy_release_test.sh docker-compose.yml
git commit -m "feat(deploy): digest限定のGo反映手順を追加" -m "Refs #125"
```

### Task 4: 本番反映を含まないGHCR release workflowを作る

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: package設定の承認を得る**

オーナーへ、送信先GHCR、private公開範囲、Go API image内容、storage/transfer費用と本番pull認証への影響を提示する。承認後にGitHub Environment `production`、required reviewer、必要secretを人間が設定する。承認前はpushを実行しない。

- [ ] **Step 2: publish jobを書く**

`on: workflow_dispatch` とmain push、permissionsはjob単位で `contents: read`、`packages: write`。login後、commit SHA tagでbuild-pushし、`docker/build-push-action` の `digest` outputをjob outputにする。latest tagは使わない。

- [ ] **Step 3: 本番反映が無いことをtestする**

workflowにSSH、Lightsail secret、`ops/deploy_release.sh`、production Environment、public healthへのcallが無いことをtestする。publish後はdigestをworkflow summaryへ出し、#126/#127が入力として使えるようにする。

- [ ] **Step 4: workflowを静的検査してコミットする**

```bash
node scripts/verify-actions-pinned.mjs
git diff --check
git diff -- .github/workflows/release.yml
git add .github/workflows/release.yml
git commit -m "ci(release): Go imageのimmutable配布を追加" -m "Refs #125"
```

### Task 5: 運用文書とPR検証を完成させる

**Files:**
- Create: `docs/deploy/ci-and-release.md`

- [ ] **Step 1: 文書を書く**

必須check名、PR/mainの権限差、GHCR package、必要secret名、digest確認方法、このIssueでは本番反映しないこと、#127のproduction Environmentとrollback入口を記載する。secret値と本番host値は書かない。

- [ ] **Step 2: 全検証を行う**

```bash
node --test scripts/verify-actions-pinned.test.mjs
node scripts/verify-actions-pinned.mjs
bash ops/deploy_release_test.sh
cd server
go test ./... -race
go build ./cmd/api ./cmd/migrate ./cmd/openapi
cd ../frontend
npm run api:check
npm run lint
npm run test
npm run build
cd ..
docker compose config
docker build -f server/Dockerfile .
```

- [ ] **Step 3: 文書をコミットする**

```bash
git add docs/deploy/ci-and-release.md
git commit -m "docs(deploy): Go APIのCIと配布手順を記録" -m "Refs #125"
```

- [ ] **Step 4: PRを作る**

PRタイトルは `ci(deploy): Go APIをimmutable imageで検査・配布する`。本文に旧自動公開の凍結、権限、固定action、image digest、local検証、未実行の本番操作を記載する。package/secret設定とworkflow dry runが完了するまで `Closes #125` を付けず、確認後に追記する。人間がレビュー・マージする。
