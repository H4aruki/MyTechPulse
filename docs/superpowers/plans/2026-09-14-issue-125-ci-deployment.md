# Issue 125 CI and Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go・Python互換・frontend・OpenAPI・DockerをPRで検証し、同一commitのAPI image・frontend成果物・運用bundleを1つのmanifestへ固定する。

**Architecture:** PRではread-only権限で全検査とimage buildを行う。mainのrelease workflowは同一checkoutからAPI image、frontend archive、compose/ops bundleを作り、release-manifest.jsonとそのSHA256を保存する。このIssueでは本番へ反映せず、#126と#127が同じmanifestと3成果物を検証して使う。

**Tech Stack:** GitHub Actions、Go 1.26、Node.js 22、Python 3.12、Docker Buildx、GHCR、Docker Compose

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- mainへ直接pushせず、人間がレビュー済みPRをマージする
- PR workflowにwrite権限や本番secretを渡さない
- actionは40桁commit SHAへ固定し、版tagはコメントとして残す
- release imageは `ghcr.io/h4aruki/mytechpulse-api-go` のdigestで本番指定する
- frontendはrelease生成CIで一度buildした成果物を使い、リハーサル・本番公開時に再buildしない
- manifestと3成果物は同一commit SHA・workflow run ID/attemptに結び付け、latestや実行時branch先頭で置き換えない
- 本番サーバー上で `docker compose build`、`go build`、`git reset` を実行しない
- 本番反映は #127 でGitHub Environment `production` の承認後だけ行う
- image/packageの公開範囲、保存費用、認証用secretの登録はオーナー承認前に変更しない
- container、image、volume、backupを自動削除しない
- Codexは本番サーバーへ直接SSH接続しない
- #124をmainへ取り込む前に、現行frontendとPython backendのmain自動公開を凍結する

---

## File Map

- Modify: `.github/workflows/ci.yml` — Go、生成差分、Docker build検査
- Create: `.github/workflows/release.yml` — 本番反映を含まない3成果物とmanifestの保存
- Create: `scripts/release-manifest.mjs`, `scripts/release-manifest.test.mjs` — manifest生成・検証
- Create: `ops/verify_release.sh`, `ops/verify_release_test.sh` — hash検証と運用bundle展開
- Create: `scripts/verify-actions-pinned.mjs`, `scripts/verify-actions-pinned.test.mjs` — action固定検査
- Create: `ops/deploy_release.sh`, `ops/deploy_release_test.sh` — manifest指定releaseの準備
- Modify: `docker-compose.yml` — local buildとdigest imageを両立
- Create: `docs/deploy/ci-and-release.md` — 必須check、secret、rollback入力

### Task 1: workflow actionのimmutable pinを検査する

**Files:**
- Create: `scripts/verify-actions-pinned.mjs`, `scripts/verify-actions-pinned.test.mjs`
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

Buildxで `server/Dockerfile` を `push: false, load: true`、cacheなしでもbuildする。最終imageから `/migrate --help` をDB設定なしで実行しexit 0を確認する。同じ最終imageの既定entrypoint `/api` を合成DB・合成設定で起動し、live/ready 200と非root実行を確認する。source上のbinaryの実行だけでは合格にしない。secretとproduction設定は渡さない。

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

### Task 3: manifest指定のrelease準備scriptをTDDで作る

**Files:**
- Create: `ops/deploy_release.sh`, `ops/deploy_release_test.sh`
- Create: `ops/verify_release.sh`, `ops/verify_release_test.sh`
- Create: `scripts/release-manifest.mjs`, `scripts/release-manifest.test.mjs`
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: `MTP_RELEASE_MANIFEST`（download済みJSONのpath）、`MTP_MANIFEST_SHA256`、`MTP_RELEASE_RUN_ID`、`MTP_RELEASE_RUN_ATTEMPT`、同じrunからdownloadしたarchive directory
- Produces: 検証済み `MTP_RELEASE_DIR`、hash照合済みfrontend archive path、manifest由来の `GO_API_IMAGE`。migration、Cloudflare公開、起動は #127 のmaintenance工程が行う
- CLI: `node scripts/release-manifest.mjs create|verify` はCIで標準Node APIだけを使う。`bash ops/verify_release.sh` は同じschemaをBash/psqlと標準hash/archiveコマンドで検証し、本番へNode/jqや新規依存を要求しない

- [ ] **Step 1: fake docker/git/curlで拒否条件を書く**

manifest未設定、manifest hash不一致、run ID/attempt不一致、tagだけ、別repository、短いdigest、frontend/ops hash不一致はexit 2。正常時はmanifest照合→両archive hash照合→release固有directoryへops展開→そのcomposeによるAPI digest pullの順とする。git更新、build、prune、migration、Caddy切替が一度も呼ばれないことを検査する。archiveの絶対path、`..`、symlink/hardlink、既存release directoryへの上書きも拒否する。

- [ ] **Step 2: composeのimage指定を追加する**

```yaml
api-go:
  image: ${GO_API_IMAGE:-mytechpulse-api-go:local}
  build:
    context: .
    dockerfile: server/Dockerfile
```

localはbuild可能。本番はops bundle内のcomposeを `-p mytechpulse -f "$MTP_RELEASE_DIR/docker-compose.yml"` で明示し、manifestから設定したdigestをpullする。project名と既存DB/Caddy volume名を固定し、release directoryの変更で別volumeを作らない。secretはrelease外の承認済み設定から渡す。

- [ ] **Step 3: 反映scriptを実装する**

`set -euo pipefail` とmanifest厳密検査を実装する。workflow側の固定された検証処理とserver側の既に承認済み検証入口でhashを確認してから、`releases/<commit_sha>-<run_id>-<run_attempt>/` の新規directoryへops bundleを展開し、frontend archiveも同directoryへ検証済み入力として配置する。その中のcompose/scriptだけを使い、未検証bundle内のscriptを検証入口として起動しない。source checkoutの更新やserver上buildに依存しない。失敗時は終了し、旧release directory・container・imageを保持する。直前のmanifest SHA256、API識別子、frontend deployment ID、frontend artifact名/hash、ops bundle名/hash/release directoryを `previous-release.json` に保存し、 #127 のrollback入力とする。初回Python版も稼働image ID/digestと実際のfrontend/compose/ops一式を保全して記録し、可変tagから再取得しない。

- [ ] **Step 4: shellとcomposeを検証してコミットする**

```bash
bash -n ops/deploy_release.sh ops/deploy_release_test.sh ops/verify_release.sh ops/verify_release_test.sh
bash ops/deploy_release_test.sh
bash ops/verify_release_test.sh
node --test scripts/release-manifest.test.mjs
docker compose config
git add ops/deploy_release.sh ops/deploy_release_test.sh ops/verify_release.sh ops/verify_release_test.sh scripts/release-manifest.mjs scripts/release-manifest.test.mjs docker-compose.yml
git commit -m "feat(deploy): digest限定のGo反映手順を追加" -m "Refs #125"
```

### Task 4: 本番反映を含まない3成果物のrelease workflowを作る

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: package設定の承認を得る**

オーナーへ、送信先GHCR、private公開範囲、Go API image内容、storage/transfer費用と本番pull認証への影響を提示する。承認後にGitHub Environment `production`、required reviewer、必要secretを人間が設定する。承認前はpushを実行しない。

- [ ] **Step 2: publish jobを書く**

`on: workflow_dispatch` とmain push、permissionsはjob単位で `contents: read`、`packages: write`。同一 `github.sha` をcheckoutし、login後commit SHA tagでAPIをbuild-pushする。最終imageの完全digestをpullし、Task 2と同じ `/api` 起動/healthと `/migrate --help` を実行する。latest tagは使わない。

同じcheckoutのfrontendを `npm ci` →契約/lint/test→ `npm run build` で一度buildし、`frontend/dist/` を `frontend-<commit_sha>.tar.gz` へ固める。公開先API URLなどbuild時設定もこの時点で固定し、#126は隔離DNS/経路でそのURLを検証APIへ向ける。再buildでURLを差し替えない。`docker-compose.yml`、`Caddyfile`、追跡済み `ops/`、存在する場合の `docker-compose.rehearsal.yml` を同じcheckoutから `ops-<commit_sha>.tar.gz` にし、秘密設定・dump・未追跡fileを含めない。

`release-manifest.json` のschemaを次に固定する（値は生成時に実値を入れる）。frontend/opsの `artifact_name` は同名archiveを含むGitHub Actions artifact名、`sha256` はarchive本体のhashである。

```json
{
  "schema_version": 1,
  "commit_sha": "<40桁commit SHA>",
  "api_image": "ghcr.io/h4aruki/mytechpulse-api-go@sha256:<64桁hex>",
  "frontend": {"artifact_name": "frontend-<commit_sha>", "file": "frontend-<commit_sha>.tar.gz", "sha256": "<64桁hex>"},
  "ops": {"artifact_name": "ops-<commit_sha>", "file": "ops-<commit_sha>.tar.gz", "sha256": "<64桁hex>"},
  "workflow": {"run_id": "<run ID>", "run_attempt": "<attempt>", "url": "<そのrunのURL>"}
}
```

両archiveとmanifestをrun/attempt単位で上書き不可のartifactへ保存し、manifest artifact名は `release-manifest-<commit_sha>-<run_attempt>` とする。manifest自身のSHA256は自己参照fieldにせず `.sha256` とworkflow summaryへ記録する。消失・期限切れ時は同名再生成で代用せず、新しいreleaseと #126 の再合格を要求する。#126/#127の運用script追加後にはそれらを含む候補を再生成し、その最終manifestを #126 で検証する。合格後のops変更も新manifestとして再検証する。

- [ ] **Step 3: 本番反映が無いことをtestする**

このpublish jobにSSH、Lightsail secret、`ops/deploy_release.sh`、production Environment、public healthへのcallが無いことをtestする。manifest生成・検証testではcommit/run不一致、欠損成果物、archiveの1byte改変、APIだけ差替え、mutable tagを拒否する。publish後はmanifest SHA256、run URL/ID/attemptと3成果物の識別子をworkflow summaryへ出し、#126/#127が入力として使えるようにする。

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

必須check名、PR/mainの権限差、GHCR package、必要secret名、manifestと3成果物のdownload/hash照合方法、保持期間、このIssueでは本番反映しないこと、#127のproduction Environmentとprevious release recordによる組合せrollbackを記載する。secret値と本番host値は書かない。

- [ ] **Step 2: 全検証を行う**

```bash
node --test scripts/verify-actions-pinned.test.mjs
node scripts/verify-actions-pinned.mjs
bash ops/deploy_release_test.sh
bash ops/verify_release_test.sh
node --test scripts/release-manifest.test.mjs
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
docker build -t mytechpulse-go:issue125 -f server/Dockerfile .
docker run --rm --entrypoint /migrate mytechpulse-go:issue125 --help
```

- [ ] **Step 3: 文書をコミットする**

```bash
git add docs/deploy/ci-and-release.md
git commit -m "docs(deploy): Go APIのCIと配布手順を記録" -m "Refs #125"
```

- [ ] **Step 4: PRを作る**

PRタイトルは `ci(deploy): Go APIをimmutable imageで検査・配布する`。本文に旧自動公開の凍結、権限、固定action、image digest、local検証、未実行の本番操作を記載する。package/secret設定とworkflow dry runが完了するまで `Closes #125` を付けず、確認後に追記する。人間がレビュー・マージする。
