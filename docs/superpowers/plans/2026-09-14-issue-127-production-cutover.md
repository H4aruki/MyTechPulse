# Issue 127 Production Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 承認済みの5〜30分メンテナンス内で、既存データを保持してGo APIと対応frontendへ切り替え、異常時はDBを巻き戻さず旧組み合わせへ戻す。

**Architecture:** Caddyを一時503へ切り替えて書き込みを止め、最終backup・snapshot・加算migration・Go readyの順に進める。frontendとAPIを同じ変更単位として公開し、中止条件ならPython APIと直前Cloudflare Pages deploymentを復帰する。

**Tech Stack:** GitHub Actions production Environment、Docker Compose、Caddy、PostgreSQL 17、Cloudflare Pages API、GHCR digest

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- #126の合格証跡と30分以内の実測が無ければ実行しない
- 本番公開前に送信先、公開範囲、内容、影響を提示し、ユーザーの明示承認を得る
- Codexは本番へ直接SSH接続せず、承認済みGitHub Actionsだけを使う
- 最終backupのchecksum検証に失敗したらmigrationを開始しない
- migrationはupだけを使い、本番rollbackでdownを実行しない
- user/tag/recommendの件数・制約・sequenceに重大差分があれば公開しない
- Go ready、主要flow、frontendのいずれかが失敗したら旧API・旧frontendを一緒に戻す
- DB volume、backup、旧image、旧Cloudflare deploymentを削除しない
- 本番Swagger UIとOpenAPIは404のままにする

---

## File Map

- Modify: `Caddyfile` — upstreamを環境変数で選択
- Modify: `docker-compose.yml` — Caddy upstreamとmaintenance file選択
- Create: `ops/Caddyfile.maintenance` — 503応答
- Create: `ops/cutover_preflight.sh`, `cutover_preflight_test.sh` — 実行前条件
- Create: `ops/cutover.sh`, `cutover_test.sh` — prepare/activateの本番server工程
- Create: `ops/rollback_release.sh`, `rollback_release_test.sh` — Python API復帰
- Create: `ops/rollback_frontend.sh`, `rollback_frontend_test.sh` — Pages復帰
- Modify: `.github/workflows/release.yml` — cutover入力とproduction承認
- Create: `docs/deploy/go-production-cutover.md` — 時刻・担当・中止条件・証跡

### Task 1: Caddyの旧/maintenance/Go切り替えを明示する

**Files:**
- Modify: `Caddyfile`, `docker-compose.yml`
- Create: `ops/Caddyfile.maintenance`

- [ ] **Step 1: 3状態のCompose config testを書く**

defaultは `api:8000`、maintenanceは `ops/Caddyfile.maintenance`、Goは `api-go:8001` をCaddy containerへ渡す。いずれも既存 `caddy_data`/`caddy_config` volume名を維持することを確認する。

- [ ] **Step 2: Caddyfileを環境変数化する**

```caddyfile
{$API_DOMAIN} {
    reverse_proxy {$API_UPSTREAM:api:8000}
    header Strict-Transport-Security "max-age=31536000; includeSubDomains"
}
```

ComposeのCaddy environmentに `API_UPSTREAM: ${API_UPSTREAM:-api:8000}`、volumeに `${CADDYFILE_PATH:-./Caddyfile}:/etc/caddy/Caddyfile:ro` を設定する。固定の `depends_on: api` は削除し、切り替えscriptが対象APIのreadyを確認してからCaddyを通常応答へ戻す。

- [ ] **Step 3: maintenance応答を書く**

`ops/Caddyfile.maintenance` は同じdomain/TLS volumeを使い、全pathへstatus 503、`Retry-After: 1800`、短い日本語plain textだけを返す。request bodyをbackendへ転送しない。

- [ ] **Step 4: 構文確認してコミットする**

```bash
docker compose config
docker run --rm -v "$PWD/Caddyfile:/etc/caddy/Caddyfile:ro" caddy:2-alpine caddy validate --config /etc/caddy/Caddyfile
docker run --rm -v "$PWD/ops/Caddyfile.maintenance:/etc/caddy/Caddyfile:ro" caddy:2-alpine caddy validate --config /etc/caddy/Caddyfile
git add Caddyfile docker-compose.yml ops/Caddyfile.maintenance
git commit -m "feat(deploy): API切り替えとmaintenance応答を追加" -m "Refs #127"
```

### Task 2: cutover前提を機械判定する

**Files:**
- Create: `ops/cutover_preflight.sh`, `cutover_preflight_test.sh`

**Interfaces:**
- Consumes: release digest、rehearsal合格記録、backup directory、旧frontend deployment ID
- Produces: 秘密値なしのpreflight pass/fail

- [ ] **Step 1: 拒否条件testを書く**

#126未完了、digest不正、checksum不一致、30分超のrehearsal、旧Python imageなし、旧Cloudflare production deployment IDなし、disk空き不足、DB unhealthy、APP_ENVがproduction以外、SWAGGER_ENABLEDがfalse以外、必須Go設定名不足の各fixtureを拒否する。

- [ ] **Step 2: preflightを実装する**

GitHub APIから #126 closedを確認し、digest完全一致、`ops/verify_backup.sh`、`docker image inspect`、`docker compose ps db`、空き容量2GiB以上、APP_ENV=production、SWAGGER_ENABLED=false、必要設定名・secret名の存在だけを検査する。設定値は比較だけに使い出力しない。Cloudflare API `GET /accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/mytechpulse/deployments?env=production&per_page=1` から現在の成功deployment IDを取得しmasked workflow outputへ保存する。

- [ ] **Step 3: testしてコミットする**

```bash
bash -n ops/cutover_preflight.sh ops/cutover_preflight_test.sh
bash ops/cutover_preflight_test.sh
git add ops/cutover_preflight.sh ops/cutover_preflight_test.sh
git commit -m "test(deploy): 本番切り替えの開始条件を固定" -m "Refs #127"
```

### Task 3: APIとfrontendのrollbackを先に実装する

**Files:**
- Create: `ops/rollback_release.sh`, `rollback_release_test.sh`
- Create: `ops/rollback_frontend.sh`, `rollback_frontend_test.sh`

**Interfaces:**
- Consumes: 旧Cloudflare deployment ID、既存Python service、Cloudflare token
- Produces: frontend rollback、Python API復帰、public health

- [ ] **Step 1: fake commandで順序testを書く**

Go公開後を初期状態とし、workflowがmaintenanceを維持したままfrontend rollback→Python API start→Caddy upstream `api:8000`→public health→Go stopの順を要求する。Cloudflare/API復帰に失敗したら非0を返し、DBやimage削除を呼ばない。

- [ ] **Step 2: Cloudflareの公式rollback APIを実装する**

```bash
curl --fail-with-body --silent --show-error \
  --request POST \
  --header "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  --header 'Content-Type: application/json' \
  --output /dev/null \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/mytechpulse/deployments/$MTP_PREVIOUS_FRONTEND_DEPLOYMENT_ID/rollback"
```

このcallはGitHub runner上の `rollback_frontend.sh` だけが行う。レスポンスbodyはログへ出さず、HTTP成功だけを記録する。rollback対象は切り替え直前に取得した成功production deploymentだけを受理する。

- [ ] **Step 3: Python API復帰を実装する**

server側の `rollback_release.sh` は `docker compose up -d --no-build api`、`API_UPSTREAM=api:8000 docker compose --profile prod up -d --no-deps --force-recreate caddy`、public healthを行う。Go追加表は残し、down migrationを実行しない。最後に `docker compose stop api-go` とする。

- [ ] **Step 4: testしてコミットする**

```bash
bash -n ops/rollback_release.sh ops/rollback_release_test.sh ops/rollback_frontend.sh ops/rollback_frontend_test.sh
bash ops/rollback_release_test.sh
bash ops/rollback_frontend_test.sh
git add ops/rollback_release.sh ops/rollback_release_test.sh ops/rollback_frontend.sh ops/rollback_frontend_test.sh
git commit -m "feat(deploy): frontendとPython APIの切り戻しを追加" -m "Refs #127"
```

### Task 4: 30分制限のcutover orchestratorを作る

**Files:**
- Create: `ops/cutover.sh`, `cutover_test.sh`

- [ ] **Step 1: phaseと中止条件testを書く**

`prepare` はmaintenance、Python stop、backup、snapshot、migration、Go readyまで、`activate` はCaddy切替、smoke、最終比較だけを行うことをfakeで確認する。backup、snapshot、migration、Go ready、smoke、data比較の各失敗は非0。開始から25分で新工程を始めず、30分で必ず非0にする。

- [ ] **Step 2: server工程を2 phaseで実装する**

`prepare` はpreflight→maintenance Caddy→Python API stop→custom dump/checksum/before snapshot→digest imageのone-shot migration→after snapshot比較→Go API起動/内部ready。`activate` は `API_UPSTREAM=api-go:8001` で通常Caddy→public ready→signup/login/me/feed/click/logout smoke→最終件数比較とする。Cloudflare公開とrollback判断はGitHub runner側workflowが両phaseの間で行う。

各工程名・経過秒・結果だけを出す。成功時もPython image/container、backup、旧frontend deploymentを保持する。

- [ ] **Step 3: testしてコミットする**

```bash
bash -n ops/cutover.sh ops/cutover_test.sh
bash ops/cutover_test.sh
git add ops/cutover.sh ops/cutover_test.sh
git commit -m "feat(deploy): Go本番切り替えを30分制限で自動化" -m "Refs #127"
```

### Task 5: production workflowとrunbookを完成させる

**Files:**
- Modify: `.github/workflows/release.yml`
- Create: `docs/deploy/go-production-cutover.md`

- [ ] **Step 1: workflowをcutover入力へ接続する**

workflow_dispatchにrelease digest、#126証跡URL、maintenance開始承認文字列を入力させる。production Environment required reviewer通過後だけcutover jobを実行する。同時実行は `concurrency: production-release`、`cancel-in-progress: false` とする。maintenance前にcheckout、`npm ci`、frontend build、preflightを完了させる。main push時の定常deploy jobは `vars.GO_DEPLOY_ENABLED == 'true'` の場合だけ同じ安全工程を使い、初回切り替え成功まではvariable未設定でskipする。

workflowはSSH経由でserverの `cutover.sh prepare` → GitHub runnerからCloudflare Pages公開 → `cutover.sh activate` の順に呼ぶ。prepare後の失敗では `rollback_frontend.sh` を必要時だけrunnerで実行し、その後SSHで `rollback_release.sh` を実行する。Cloudflare tokenを本番serverへ渡さない。

- [ ] **Step 2: runbookへ責任と中止条件を書く**

担当者、利用者告知、開始/終了時刻、backup checksum、before/after件数、Go/frontend版、旧frontend deployment ID、各health、rollback結果、最終判定を記録する。実値はIssueコメントに残し、secretや個人dataは書かない。

- [ ] **Step 3: 静的検証する**

```bash
node scripts/verify-actions-pinned.mjs
bash ops/cutover_test.sh
bash ops/rollback_release_test.sh
bash ops/rollback_frontend_test.sh
git diff --check
```

- [ ] **Step 4: 文書とworkflowをコミットする**

```bash
git add .github/workflows/release.yml docs/deploy/go-production-cutover.md
git commit -m "docs(deploy): Go本番切り替え手順を確定" -m "Refs #127"
```

- [ ] **Step 5: 実行基盤PRを先にマージしてもらう**

PRタイトルは `feat(deploy): Go本番切り替えと切り戻しを自動化する`。本文は `Refs #127` とし、この時点ではIssueをcloseしない。人間のレビュー・マージ後、main上のworkflowだけをTask 6で使う。

### Task 6: 明示承認後に本番切り替えを実行する

- [ ] **Step 1: 公開内容を提示して承認を待つ**

送信先は `api.mytechpulse.net` とCloudflare Pages、公開範囲は一般利用者、内容はGo API・Cookie対応frontend・加算DB migration、影響は5〜30分の503と全利用者の再loginであることを提示する。ユーザーがこの本番実行を明示承認するまでworkflowを開始しない。

- [ ] **Step 2: 承認済みworkflowを1回実行する**

承認されたdigestと #126証跡を指定し、GitHub Actionsからworkflow_dispatchする。Codexは実行statusと公開healthだけを監視し、SSH接続しない。

- [ ] **Step 3: data・機能・非公開経路を確認する**

Issueへbefore/after件数一致、既存利用者のlogin、記事、click、logout、新規登録、`/docs` 404、`/openapi.json` 404、`/openapi.yaml` 404、`/schemas/` 配下404、所要時間を記録する。実利用者名やレスポンスbodyは記録しない。

- [ ] **Step 4: 成否を確定する**

中止条件ならrollback完了まで監視し、原因修正と #126再実施までIssueをopenにする。成功なら旧構成を残したまま監視期間へ入り、ユーザーがGitHub repository variable `GO_DEPLOY_ENABLED=true` を設定して以後のmain releaseをGo経路へ有効化する。証跡コメントを付けて #127をcloseする。

Cloudflare Pagesは成功済みproduction deploymentへrollbackできるため、直前deployment IDを切り替え前に保存する。公式仕様: https://developers.cloudflare.com/api/resources/pages/subresources/projects/subresources/deployments/methods/rollback/
