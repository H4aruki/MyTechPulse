# Issue 127 Production Cutover Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** #126で合格した1つのrelease manifestと3成果物を使い、承認済みの5〜30分メンテナンス内で既存データを保持してGo APIと対応frontendへ切り替え、異常時はDBを巻き戻さず直前release一式へ戻す。

**Architecture:** production workflowは #126の合格証跡に固定されたmanifest SHA256・release run ID/attemptからAPI digest、frontend archive、ops bundleを再取得してhash検証する。検証済みops bundleをrelease固有directoryへ展開し、Caddyを503にして書き込みを止めたまま最終backup、before snapshot、加算migration、after内容比較、Go内部ready、限定経路smoke、合成data cleanup、cleanup後内容比較まで完了する。その後だけ同じfrontend archiveをCloudflareへ公開して通常経路をGoへ開き、異常時はprevious release recordのAPI・frontend・opsを組み合わせで復帰する。

**Tech Stack:** GitHub Actions production Environment、Docker Compose、Caddy、PostgreSQL 17、Cloudflare Pages API、GHCR digest

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- #126の合格証跡、manifest SHA256、release元workflow run ID/attempt、rehearsal run URL/ID、30分以内の実測が無ければ実行しない
- #126で合格した同一manifestのAPI完全digest、frontend archive、ops bundleだけを使い、APIだけの差替え、latest、source checkoutの先頭を使わない
- 本番公開前に送信先、公開範囲、内容、影響を提示し、ユーザーの明示承認を得る
- Codexは本番へ直接SSH接続せず、承認済みGitHub Actionsだけを使う
- 本番serverでgit pull、source build、frontend buildを行わない
- 最終backupのchecksum検証に失敗したらmigrationを開始しない
- migrationはupだけを使い、本番rollbackでdownを実行しない
- user/tag/recommendの件数・制約・sequence・既存行内容の比較に不一致があれば公開しない
- 一般利用者の書き込み再開前に、Go内部ready、限定経路smoke、合成data cleanup、cleanup後内容比較を完了する
- Go ready、主要flow、frontendのいずれかが失敗したらprevious release recordのAPI・frontend・opsを一緒に戻す
- DB volume、backup、旧image、旧Cloudflare deployment、直前release directoryを削除しない
- 本番Swagger UIとOpenAPIは404のままにする

---

## File Map

- Modify: `Caddyfile` — upstreamを環境変数で選択
- Modify: `docker-compose.yml` — Caddy upstreamとmaintenance file選択
- Create: `ops/Caddyfile.maintenance` — 503応答
- Create: `ops/cutover_preflight.sh`, `ops/cutover_preflight_test.sh` — 実行前条件
- Create: `ops/cutover.sh`, `ops/cutover_test.sh` — prepare/activateの本番server工程とdata保全
- Create: `ops/rollback_release.sh`, `ops/rollback_release_test.sh` — previous releaseのAPI・ops復帰
- Create: `ops/rollback_frontend.sh`, `ops/rollback_frontend_test.sh` — previous releaseのPages復帰
- Modify: `.github/workflows/release.yml` — 合格manifest入力、3成果物検証、production承認
- Create: `docs/deploy/go-production-cutover.md` — 時刻・担当・中止条件・証跡

### Task 1: Caddyの旧/maintenance/Go切り替えを明示する

**Files:**
- Modify: `Caddyfile`, `docker-compose.yml`
- Create: `ops/Caddyfile.maintenance`

- [ ] **Step 1: 3状態のCompose config testを書く**

defaultは `api:8000`、maintenanceは `ops/Caddyfile.maintenance`、Goは `api-go:8001` をCaddy containerへ渡す。いずれも既存 `caddy_data`/`caddy_config` volume名を維持すること、検証済みrelease directoryのcompose/Caddyfileを明示し、checkout側の同名fileやbuild節へfallbackしないことを確認する。

- [ ] **Step 2: Caddyfileを環境変数化する**

```caddyfile
{$API_DOMAIN} {
    reverse_proxy {$API_UPSTREAM:api:8000}
    header Strict-Transport-Security "max-age=31536000; includeSubDomains"
}
```

ComposeのCaddy environmentに `API_UPSTREAM: ${API_UPSTREAM:-api:8000}`、volumeに `${CADDYFILE_PATH:-./Caddyfile}:/etc/caddy/Caddyfile:ro` を設定する。本番では `-p mytechpulse -f "$MTP_RELEASE_DIR/docker-compose.yml"` とrelease directory内のCaddyfileを必須にする。固定の `depends_on: api` は削除し、切り替えscriptが対象APIのreadyとdata保全を確認してからCaddyを通常応答へ戻す。

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
- Create: `ops/cutover_preflight.sh`, `ops/cutover_preflight_test.sh`

**Interfaces:**
- Consumes: `MTP_MANIFEST_SHA256`、`MTP_RELEASE_RUN_ID`、`MTP_RELEASE_RUN_ATTEMPT`、#126合格証跡URL/run ID、download済みmanifestとfrontend/ops archive、backup directory、`previous-release.json`
- Produces: 検証済みrelease directory、manifest由来のAPI完全digestとfrontend archive path、秘密値なしのpreflight pass/fail

- [ ] **Step 1: 拒否条件testを書く**

#126未完了、合格証跡のmanifest SHA256/release run ID/attempt不一致、rehearsal run ID不足、3成果物の欠損/hash不一致、API digest不正、APIだけの差替え、30分超のrehearsal、previous releaseのAPI/frontend/opsいずれか不足、disk空き不足、DB unhealthy、APP_ENVがproduction以外、SWAGGER_ENABLEDがfalse以外、必須Go設定名不足の各fixtureを拒否する。`latest`、checkout側compose、git pull、server/frontend buildへfallbackするfixtureも失敗させる。

- [ ] **Step 2: preflightを実装する**

GitHub APIから #126 closedと合格証跡を確認し、証跡に記録されたmanifest SHA256、release元run ID/attempt、rehearsal run URL/IDがworkflow入力と一致することを検査する。そのrelease runからmanifest、frontend artifact、ops artifactをdownloadし、#125の固定検証入口でmanifest自身と両archiveのSHA256、commit SHA、run ID/attempt、API完全digestを照合する。検証後だけops bundleを `releases/<commit_sha>-<run_id>-<run_attempt>/` へ新規展開し、そのdirectoryのcompose/scriptを以後の入力にする。既存directoryは上書きしない。

続けて `ops/verify_backup.sh`、API digestのpull/inspect、`docker compose ps db`、空き容量2GiB以上、APP_ENV=production、SWAGGER_ENABLED=false、必要設定名・secret名の存在だけを検査する。設定値は比較だけに使い出力しない。Cloudflare API `GET /accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/mytechpulse/deployments?env=production&per_page=1` から現在の成功deployment IDを取得し、直前manifest SHA256（初回Python版はnull）、APIのimage ID/digestとservice名、frontend deployment IDとartifact名/hash、ops artifact名/hash/release directoryを `previous-release.json` へ保存する。recordのschema versionは1とし、`api`、`frontend`、`ops` の3objectを必須にする。workflow outputは機密値を含めず、値が必要な場合はmaskする。

- [ ] **Step 3: testしてコミットする**

```bash
bash -n ops/cutover_preflight.sh ops/cutover_preflight_test.sh
bash ops/cutover_preflight_test.sh
git add ops/cutover_preflight.sh ops/cutover_preflight_test.sh
git commit -m "test(deploy): 本番切り替えの開始条件を固定" -m "Refs #127"
```

### Task 3: previous release一式のrollbackを先に実装する

**Files:**
- Create: `ops/rollback_release.sh`, `ops/rollback_release_test.sh`
- Create: `ops/rollback_frontend.sh`, `ops/rollback_frontend_test.sh`

**Interfaces:**
- Consumes: 検証済み `previous-release.json`、直前release directory、Cloudflare token
- Produces: 直前frontend deployment、直前ops構成、直前APIの組合せ復帰とpublic health

- [ ] **Step 1: fake commandで順序testを書く**

Go公開後を初期状態とし、workflowがmaintenanceを維持したまま `previous-release.json` を検証し、frontend rollback→直前release directoryのcompose/opsで旧API start→旧Caddy設定→public health/主要read flow→候補API stopの順を要求する。初回Python版と将来のGo版の両record fixtureを用意し、固定のservice名や可変tagへ推測で戻らないことを検査する。frontend/API/opsのどれかが欠ける、または復帰に失敗したら非0を返し、DB migration、volume、backup、release directory、container imageの削除を呼ばない。

- [ ] **Step 2: Cloudflareの公式rollback APIを実装する**

```bash
curl --fail-with-body --silent --show-error \
  --request POST \
  --header "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
  --header 'Content-Type: application/json' \
  --output /dev/null \
  "https://api.cloudflare.com/client/v4/accounts/$CLOUDFLARE_ACCOUNT_ID/pages/projects/mytechpulse/deployments/$MTP_PREVIOUS_FRONTEND_DEPLOYMENT_ID/rollback"
```

このcallはGitHub runner上の `rollback_frontend.sh` だけが行う。レスポンスbodyはログへ出さず、HTTP成功だけを記録する。rollback対象は `previous-release.json` に切り替え直前のmanifest/API/opsと組み合わせて保存した成功production deploymentだけを受理する。

- [ ] **Step 3: 直前API・ops復帰を実装する**

server側の `rollback_release.sh` はrecordを検証して `api.service` を `MTP_PREVIOUS_API_SERVICE`、`ops.release_directory` を `MTP_PREVIOUS_RELEASE_DIR` に設定し、`docker compose -p mytechpulse -f "$MTP_PREVIOUS_RELEASE_DIR/docker-compose.yml" up -d --no-build "$MTP_PREVIOUS_API_SERVICE"`、recordのCaddy/ops設定によるrouting復帰、public health/主要read flowを行う。初回Python版は保全済みimage ID/digestとops一式、以後は直前manifestの完全digestとops bundleを使う。候補releaseのcomposeやcheckoutは使わない。Goが追加したDB変更は残し、down migrationを実行しない。確認後だけ候補APIをstopする。

- [ ] **Step 4: testしてコミットする**

```bash
bash -n ops/rollback_release.sh ops/rollback_release_test.sh ops/rollback_frontend.sh ops/rollback_frontend_test.sh
bash ops/rollback_release_test.sh
bash ops/rollback_frontend_test.sh
git add ops/rollback_release.sh ops/rollback_release_test.sh ops/rollback_frontend.sh ops/rollback_frontend_test.sh
git commit -m "feat(deploy): 直前release一式の切り戻しを追加" -m "Refs #127"
```

### Task 4: 30分制限のcutover orchestratorを作る

**Files:**
- Create: `ops/cutover.sh`, `ops/cutover_test.sh`

**Interfaces:**
- Consumes: 検証済み `MTP_RELEASE_DIR`、manifest由来のAPI digest/frontend archive、#126のsnapshot/comparator、`MTP_SYNTHETIC_CLEANUP_APPROVED=DELETE_CUTOVER_SYNTHETIC_DATA`、production設定
- Produces: `prepare` の保全判定と限定smoke結果、`activate` の通常経路切替結果。公開ログは工程名・秒数・pass/failと `{"matches":<bool>,"mismatched_tables":<number>}` だけ

- [ ] **Step 1: phaseと中止条件testを書く**

`prepare` は検証済みrelease directoryのpreflight→maintenance→旧API stop→backup/checksum→before snapshot→migration→after snapshot→内容比較→Go内部ready→loopback/限定経路のfrontend+API smoke→期待差分検査→承認済みcleanup→cleanup snapshot→afterとの内容比較まで行う。`activate` はCloudflareへ同じfrontend archiveが公開済みであることを確認し、候補opsのCaddyをGoへ切り替えてpublic live/readyと非公開経路404だけを確認する。一般利用者の通常経路が開く前に合成writeとcleanupが終わることをfakeの呼出し順で固定する。

manifest/3成果物、backup、snapshot、migration、内容比較、Go ready、限定smoke、期待差分、cleanup、cleanup後比較、frontend deployment確認の各失敗は非0とし、`activate` と通常Caddyを呼ばない。cleanup承認値が無い場合も合成write前に失敗する。開始から25分で新工程を始めず、30分で必ず非0にする。git、build、latest、public signup/clickを一度も呼ばないことも検査する。

- [ ] **Step 2: server工程を2 phaseで実装する**

`prepare` は検証済みrelease directoryを明示してpreflight→maintenance Caddy→旧API stop→PostgreSQL custom dump/checksum→秘密nonce生成→before snapshot→manifestのAPI digestによるone-shot migration→after snapshot→既存3表の内容比較→同digestのGo API起動/内部readyの順に行う。snapshotは #126 と同じSQL/wrapperを使い、`user(user_ID,user_name,password)`、`tag(tag_ID,tag_name)`、`recommend(user_ID,tag_ID,match_int)` を同じnonceで比較する。raw値、利用者名、password hash、tag、nonce、row/table digest、snapshot JSONを通常ログ・artifactへ出さない。比較公開出力は一致可否と不一致table数だけにする。

既存dataのafter比較に合格してから、manifestのfrontend archiveをloopbackまたはworkflowだけが到達できる限定経路で配信し、Go APIもpublic Caddyを経由せず内部network/限定hostから確認する。合成利用者のsignup/login/me/feed/click/logoutを実行し、既存行に触れず「userが合成1件増加、tagは不変、recommendは選択した既存tagに対応する合成利用者の行だけ増加・更新」を検査する。既存利用者でclickを行わない。

合成dataのcleanup対象は「このrunで作成した合成利用者1件、同利用者に属するrecommend行、残存する同利用者のauth_session」に限定し、既存user、既存tag、他利用者のrecommendを削除しない。対象・役割・理由・影響をTask 6のcutover承認へ含め、`MTP_SYNTHETIC_CLEANUP_APPROVED` が明示承認済みの固定値 `DELETE_CUTOVER_SYNTHETIC_DATA` と完全一致する場合だけ削除する。cleanup後に同じnonceでsnapshotを取り、migration直後のafter snapshotと既存3表の内容・件数・制約・sequenceが完全一致してから `prepare` を成功にする。cleanupを承認できない、または一致しない場合はmaintenanceを維持して公開せずrollbackへ進む。

GitHub runnerは `prepare` 成功後もpublic Caddyを503のまま維持し、hash照合済みfrontend archiveを再buildせずCloudflare Pages productionへ送る。deployment成功と対象artifact hashを確認した後だけ `activate` を呼ぶ。`activate` は候補ops bundleのCaddy設定で `API_UPSTREAM=api-go:8001` へ切り替え、public live/readyと `/docs`、`/openapi.json`、`/openapi.yaml`、`/schemas/` の404を確認する。public経路ではsignup/clickを再実行しない。

各工程名・経過秒・結果だけを出す。成功時もprevious releaseのAPI container/image、frontend deployment、ops directory、backupを保持する。nonce/snapshot/error fileは非公開一時directoryだけに置き、このrunで作成したものだけを終了時に削除する。

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

workflow_dispatchの入力名を `manifest_sha256`、`release_run_id`、`release_run_attempt`、`rehearsal_evidence_url`、`rehearsal_run_id`、`maintenance_approval`、`cleanup_approval` とする。`maintenance_approval` は `START_GO_CUTOVER`、`cleanup_approval` は `DELETE_CUTOVER_SYNTHETIC_DATA` との完全一致を必須にする。production Environment required reviewer通過後だけcutover jobを実行する。同時実行は `concurrency: production-release`、`cancel-in-progress: false` とする。入力したrelease runからmanifest、frontend artifact、ops artifactをdownloadし、合格証跡との一致と3成果物のhashを再検証する。manifest外のdigest、別runのartifact、APIだけの上書きは受理しない。

maintenance前に検証済み成果物をrelease固有directoryへ準備し、previous release record、backup余地、DB health、production設定をpreflightする。`npm ci`、`npm run build`、`docker compose build`、`go build`、本番serverのgit pullは行わない。frontendはmanifestのarchiveをそのままCloudflareへ送信し、ops/composeはmanifestのbundleだけを使う。main pushは3成果物を生成するだけで初回切り替えを開始せず、本番cutoverは明示的なworkflow_dispatchとEnvironment承認に限定する。

workflowはSSH経由で検証済みrelease directoryの `cutover.sh prepare` → GitHub runnerから同じfrontend archiveをCloudflare Pages productionへ送信・deployment確認 → `cutover.sh activate` の順に呼ぶ。`prepare` が限定smoke、cleanup、cleanup後内容比較まで完了する前にCloudflare公開や通常Caddy切替を行わない。prepare後の失敗では、frontend deploymentを変更済みの場合だけrunnerでprevious recordの `rollback_frontend.sh` を実行し、その後SSHでprevious release directoryを指定して `rollback_release.sh` を実行する。Cloudflare tokenを本番serverへ渡さず、API・frontend・opsを別々の候補へ差し替えない。

- [ ] **Step 2: runbookへ責任と中止条件を書く**

担当者、利用者告知、開始/終了時刻、manifest SHA256、commit SHA、release元run URL/ID/attempt、rehearsal run URL/ID、API完全digest、frontend/ops artifact名とSHA256、release directory、backup checksum、before/after/cleanup後の件数・制約・sequence・内容一致可否と不一致table数、限定smoke、合成write期待差分、cleanup承認と結果、Cloudflare deployment ID、各health、previous release record、rollback結果、最終判定を記録する。実値はIssueコメントに残すが、raw値、利用者名、password hash、tag、nonce、row/table digest、snapshot JSON、secretは書かない。

- [ ] **Step 3: 静的検証する**

```bash
node scripts/verify-actions-pinned.mjs
bash ops/verify_release_test.sh
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

- [ ] **Step 6: 最終opsを含むreleaseを再生成して #126を再実施する**

Task 1〜5のcutover、rollback、snapshot、検証scriptを含むmainの同一commitから #125のrelease workflowを実行し、新しいmanifestと3成果物を生成する。この操作では本番反映しない。生成されたmanifest SHA256・release run ID/attemptを入力して #126の全リハーサルを再実施し、同じAPI digest、frontend archive、ops bundleで合格したrehearsal run URL/IDを記録する。合格後にops、frontend、APIのいずれかが変わった場合は、新しいmanifest生成と #126を再度行う。

### Task 6: 明示承認後に本番切り替えを実行する

- [ ] **Step 1: 公開内容を提示して承認を待つ**

送信先は `api.mytechpulse.net` とCloudflare Pages、公開範囲は一般利用者、内容は合格manifestに固定されたGo API・Cookie対応frontend・ops bundle・加算DB migration、影響は5〜30分の503と全利用者の再loginであることを提示する。使用するmanifest SHA256、release run URL/ID/attempt、rehearsal証跡URL/run ID、3成果物の識別子も示す。

同じ承認でcleanupについて次の4点を明記する。(1) 対象: このcutover runが作る合成利用者1件、その利用者のrecommend行とauth_session行。(2) 役割: 本番限定経路でsignup/click/logoutを確認する一時検証data。(3) 理由: smoke後にmigration直後の既存3表へ戻して内容不変を再確認するため。(4) 影響: 合成利用者にだけ属する行を削除し、既存利用者・既存tag・他利用者のrecommend・追加migrationは残す。公開実行とこのcleanupの両方をユーザーが明示承認するまでworkflowを開始しない。

- [ ] **Step 2: 承認済みworkflowを1回実行する**

承認されたmanifest SHA256、release元run ID/attempt、rehearsal証跡URL/run ID、maintenance/cleanup承認文字列を指定し、GitHub Actionsからworkflow_dispatchする。workflowが同じmanifestのAPI digest、frontend archive、ops bundleを再検証したことを確認する。Codexは実行statusと公開healthだけを監視し、SSH接続しない。

- [ ] **Step 3: data・機能・非公開経路を確認する**

Issueへbefore/afterの既存3表内容一致、Go内部ready、通常公開前の限定経路での既存利用者login/記事表示（clickなし）、合成利用者signup/click/logoutの期待差分、承認済みcleanup、cleanup後とafter snapshotの内容一致、Cloudflareへ同じfrontend artifactを送った結果、public live/ready、`/docs` 404、`/openapi.json` 404、`/openapi.yaml` 404、`/schemas/` 配下404、所要時間を記録する。比較結果は一致可否と不一致table数だけとし、実利用者名、合成値、password hash、tag、nonce、row/table digest、snapshot、レスポンスbodyは記録しない。

- [ ] **Step 4: 成否を確定する**

中止条件ならprevious release recordによるAPI・frontend・opsの組合せrollback完了まで監視し、追加DB変更は残す。原因修正でmanifest、frontend、ops、APIのいずれかが変わる場合は新しいmanifestを作り #126を再実施するまでIssueをopenにする。成功なら直前release一式とbackupを残したまま監視期間へ入り、証跡コメントを付けて #127をcloseする。以後の自動公開有効化はこの初回cutoverに含めず、別途同じrelease単位・承認条件を定める。

Cloudflare Pagesは成功済みproduction deploymentへrollbackできるため、直前deployment IDを切り替え前に保存する。公式仕様: https://developers.cloudflare.com/api/resources/pages/subresources/projects/subresources/deployments/methods/rollback/
