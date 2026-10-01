# Issue 126 Migration Rehearsal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 本番切り替え前に、復元DBと1つのrelease manifestに固定された3成果物を使ってmigration、既存データ内容保全、主要操作、性能、切り戻しを再現し、停止時間30分以内を実測する。

**Architecture:** 本番とは隔離したrehearsal Compose projectを使う。自動scriptは同一manifestのAPI digest・frontend artifact・ops bundleをdownload/hash検証し、件数・制約・sequenceと秘密nonce付きのデータ内容比較を行う。ブラウザ操作と本番相当バックアップの利用は人間が承認・実行し、機密値を含まない結果だけをIssueへ記録する。

**Tech Stack:** Docker Compose、PostgreSQL 17、Bash、Go release image、React production build、GitHub Issues

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- 本番サーバー・本番DBへ接続しない
- rehearsalは既存Compose project、DB volume、portと別名を使う
- 本番由来dumpをrepositoryへ追加せず、内容・利用者名・password hashをログへ出さない
- 入力dumpと既存backupを削除・上書きしない
- migration後の件数、FK、unique、sequenceに加え、既存user/tag/recommendのキーと値をmigration前と比較する
- #125の1つのmanifest SHA256・workflow run ID/attemptを入力し、指定された3成果物を使う。APIだけの差替えとsource/frontend再buildは禁止
- 合格条件は停止相当工程30分以内、主要flow全成功、rollback全成功、重大なdata差分0
- 合格しない状態で #127 を開始しない

---

## File Map

- Create: `docker-compose.rehearsal.yml` — 隔離DB/API/Caddy構成
- Create: `ops/rehearsal.sh`, `ops/rehearsal_test.sh` — 全工程orchestrator
- Create: `ops/sql/snapshot_migration_state.sql` — DB内のnonce付き内容digestと件数・制約snapshot
- Create: `ops/snapshot_migration_state.sh`, `ops/snapshot_migration_state_test.sh` — nonce管理・非公開snapshot
- Create: `ops/compare_migration_state.sh`, `ops/compare_migration_state_test.sh` — 前後比較
- Create: `ops/rehearsal_smoke.sh`, `ops/rehearsal_smoke_test.sh` — healthとAPI flow
- Create: `docs/deploy/go-migration-rehearsal.md` — 人間向けchecklist・記録欄

### Task 1: dataを露出しないmigration snapshotを作る

**Files:**
- Create: `ops/sql/snapshot_migration_state.sql`
- Create: `ops/snapshot_migration_state.sh`, `ops/snapshot_migration_state_test.sh`
- Create: `ops/compare_migration_state.sh`, `ops/compare_migration_state_test.sh`

**Interfaces:**
- Consumes: `MTP_SNAPSHOT_NONCE_FILE`（1実行で生成する秘密nonceの0600 file）、`MTP_SNAPSHOT_OUTPUT`（0700一時directory内の0600 file）、検証対象DB
- Produces: 非公開snapshot JSON `{schema_version, counts, constraints, sequences, table_digests}`。`table_digests` はDB内で計算した3表のaggregateだけで、raw値・row digest・nonceは含めない
- Comparator: `bash ops/compare_migration_state.sh "$before" "$after"` は公開出力を `{"matches":true,"mismatched_tables":0}` の2項目に限定し、不一致/検査不能は非0。JSON file自体をCI artifact・Issue・通常ログへ出さない

- [ ] **Step 1: 比較scriptの失敗テストを書く**

同じuser/tag/recommendの内容・件数は成功、1件差・required FK/unique/check不足・各sequence `last_value < max_id` は失敗。合成DBで件数を維持したままuser_name/password/tag_name/match_intを1値だけ変える各case、user_ID/tag_IDの変更と関連キー更新、recommendの複合キーだけの変更をそれぞれ失敗させる。空3表同士、物理行順を変えた同じ内容は成功、空→1行は失敗。NULL/空文字、区切り文字、Unicodeを含む合成値の曖昧な連結も検出する。異なるnonceで作ったsnapshotは拒否し、同じnonceでbefore/after/cleanup比較まで実行する。stdout/stderr/artifact候補に合成password hash、名前、tag、nonce、row/table digestが現れないことも検査する。

- [ ] **Step 2: snapshot SQLを書く**

SQLは3表の `count(*)`、PK/FK/unique/check制約名と定義、sequenceのlast_value/is_calledと各IDのmaxを取得する。内容比較は以下の列をDB内で順序付きJSON arrayへ正規化し、PostgreSQL 17組み込み `sha256(bytea)` と `convert_to(..., 'UTF8')` で計算する。追加extensionは使わない。role/auth_sessionの追加は既存内容の比較対象外で、別途追加スキーマとして検査する。

| 表 | rowの正規化列順 | table集約時の数値キー順 |
| --- | --- | --- |
| user | user_ID, user_name, password | user_ID |
| tag | tag_ID, tag_name | tag_ID |
| recommend | user_ID, tag_ID, match_int | user_ID, tag_ID |

SQLの計算核は次の形とし、3表とも同じ規則を使う。`snapshot_nonce` は一時表で1行だけ、`n` は秘密nonceである。

```sql
WITH rows AS (
  SELECT u."user_ID" AS id,
         encode(sha256(convert_to(n || ':row:user:' ||
           jsonb_build_array(u."user_ID", u.user_name, u.password)::text,
           'UTF8')), 'hex') AS row_digest
  FROM public."user" AS u CROSS JOIN snapshot_nonce
)
SELECT encode(sha256(convert_to(n || ':table:user:' ||
  coalesce((SELECT string_agg(row_digest, '' ORDER BY id) FROM rows), ''),
  'UTF8')), 'hex') AS table_digest
FROM snapshot_nonce;
```

tag/recommendもtable名をdomain separatorに含め、recommendは2キーで数値sortする。固定長hexの連結とJSONの型・NULL表現で区切りや並びの曖昧さをなくす。空tableは空連結を同じnonceでhashする。row digestはDB外へ返さず、table aggregateだけを非公開snapshotへ保存する。raw password hashや利用者名、tag名をSQL結果/JSON/logへ返さない。

- [ ] **Step 3: comparatorを実装する**

Nodeやjqを追加せず、PostgreSQLへ2 JSONを標準入力のCOPYで渡し、`jsonb` 演算で比較するBash wrapperにする。migrationで意図的に増えるrole/auth_session制約はafter必須、3表の件数・既存制約・sequence状態・table digestは完全一致、auth_session件数は既存内容の比較対象外とする。制約・sequence異常も当該表の不一致として数え、比較結果は一致可否と不一致table数だけを出す。

snapshot wrapperは `umask 077` の一時directoryに `/dev/urandom` 由来32byteのnonceをhexで生成し、before/after/cleanupで同じfileを再利用する。nonce識別用の秘密でない実行IDをwrapperの状態に持ち、他実行snapshotの混在を拒否する。nonceは引数・SQL文字列・環境変数・shell traceへ展開せず、標準入力のCOPYで一時表へ渡す。`psql -X -q` を使い、SQL/parameter/statement errorのログにCOPYデータが出ない設定を前提検査し、満たせなければ停止する。DB接続失敗等のstderrは保護された一時fileで受け、公開するのは固定の失敗コードだけとする。snapshot/digestは機密扱いでartifact登録しない。処理終了時はこの実行で作成したnonce/snapshot/一時error fileだけを消し、再試行時は新nonceでbeforeからやり直す。

- [ ] **Step 4: 合成fixtureで検証してコミットする**

```bash
bash -n ops/compare_migration_state.sh ops/compare_migration_state_test.sh
bash ops/compare_migration_state_test.sh
bash ops/snapshot_migration_state_test.sh
git add ops/sql/snapshot_migration_state.sql ops/snapshot_migration_state.sh ops/snapshot_migration_state_test.sh ops/compare_migration_state.sh ops/compare_migration_state_test.sh
git commit -m "test(migration): DB前後状態の比較を追加" -m "Refs #126"
```

### Task 2: 隔離rehearsal環境を作る

**Files:**
- Create: `docker-compose.rehearsal.yml`

**Interfaces:**
- Consumes: 検証済みmanifest由来の `MTP_REHEARSAL_IMAGE`、frontend展開directory、ops release directory、`MTP_REHEARSAL_DUMP`、合成password
- Produces: project `mytechpulse-rehearsal`、loopback ports 18001/15432、frontend配信と隔離API routing

- [ ] **Step 1: Compose設定testを書く**

```bash
MTP_REHEARSAL_IMAGE='ghcr.io/h4aruki/mytechpulse-api-go@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
MTP_REHEARSAL_DB_PASSWORD='synthetic-rehearsal-only' \
docker compose -p mytechpulse-rehearsal -f docker-compose.rehearsal.yml config
```

出力にproduction volume名、port 8000/8001/5432、build節が無いことを文字列検査する。

- [ ] **Step 2: 隔離構成を書く**

DBはnamed volume `mytechpulse_rehearsal_db`、host bind `127.0.0.1:15432`。APIは `MTP_REHEARSAL_IMAGE` 必須、host bind `127.0.0.1:18001`、APP_ENV test、Swagger有効。migrationは同じdigestのimageで `/migrate` entrypointを一度実行する。manifestのfrontend archiveをhash照合して展開し、同じops bundle内のCaddy/composeで配信する。releaseのAPI URLは変更せず、隔離環境だけの名前解決/TLS経路を検証APIへ向ける。本番host/DBへ到達しないことを先に検査する。production Cookie/Origin設定での検査もこの限定経路で実施する。

- [ ] **Step 3: image値をdigest限定にする**

Compose実行前に #125 の `ops/verify_release.sh` でmanifest SHA256、run ID/attempt、frontend/ops archive hashを照合する。APIはmanifestの完全digestを使い、latestやcommit tagだけでは起動しない。composeと運用scriptは検証済みops directoryから使い、checkoutの同名fileへ切り替えない。

- [ ] **Step 4: config検証してコミットする**

```bash
docker compose -p mytechpulse-rehearsal -f docker-compose.rehearsal.yml config
git add docker-compose.rehearsal.yml
git commit -m "feat(migration): 隔離リハーサル環境を追加" -m "Refs #126"
```

### Task 3: 認証・feed・clickのsmoke testを作る

**Files:**
- Create: `ops/rehearsal_smoke.sh`, `ops/rehearsal_smoke_test.sh`

**Interfaces:**
- Consumes: `MTP_REHEARSAL_BASE_URL`、合成username/password
- Produces: signup/login/me/feed/click/logout/401のpass/fail

- [ ] **Step 1: fake curlで呼び出し順testを書く**

health live/ready→signup→me→feed→click→logout→me 401の順を確認する。Cookie jarは `mktemp` fileだけに保存し、trapでその一時fileを削除する。標準出力にpassword/Cookie/body全体を出さない。

- [ ] **Step 2: HTTP statusとJSON key検査を実装する**

signup 201、me 200、feed 200か部分成功200、click 204、logout 204、logout後me 401を要求する。feedの `qiita_articles`/`zenn_articles` をdecodeし、URLはhttpsかつqiita.com/zenn.devだけ、sourceはQiita/Zennだけ、内部score fieldが無いことをGo小commandで確認する。

- [ ] **Step 3: script testしてコミットする**

```bash
bash -n ops/rehearsal_smoke.sh ops/rehearsal_smoke_test.sh
bash ops/rehearsal_smoke_test.sh
git add ops/rehearsal_smoke.sh ops/rehearsal_smoke_test.sh
git commit -m "test(migration): Go APIの移行smokeを追加" -m "Refs #126"
```

### Task 4: 復元からrollbackまでを1本化する

**Files:**
- Create: `ops/rehearsal.sh`, `ops/rehearsal_test.sh`

- [ ] **Step 1: 前提・失敗停止testを書く**

dump不存在、checksum不一致、manifest SHA256/run不一致、3成果物の欠損/hash不一致、APIだけの差替えは起動前に失敗。restore/migrate/smoke/comparisonのどれかが失敗したら後続切り替えをせず非0。previous release recordに対応する旧API・frontend・opsの組合せへ戻し、healthとブラウザflowを確認したときだけrollback passとする。

- [ ] **Step 2: 工程を実装する**

指定run/attemptからmanifestと同じfrontend/ops artifactをdownloadし、入力manifest SHA256と両archive hashを検証する。APIを完全digestでpullした後、開始epochを記録し、backup検証→隔離DB起動→新規DBへrestore→before snapshot→migration→after snapshot→内容比較→Go起動→そのfrontend/ops/APIの組合せでsmoke→旧release組合せへ戻す模擬→health/画面→Go release組合せへ再切替→healthの順にする。各工程は名前・秒数・pass/failだけを出す。一般利用者の書込みを遮断したまま内容比較する #127 の順序を再現し、合成writeによる期待差分と承認済みcleanup後の3表内容一致も検証する。cleanupは合成利用者とその関連行だけを対象にし、既存tagを削除しない。

- [ ] **Step 3: 終了処理を安全にする**

自動終了はcontainer stopだけを行い、input dumpとnamed volumeは保持する。実行時に作った一時Cookie/snapshot fileだけをtrapで削除する。volumeを削除する `down -v` はscriptへ入れない。

- [ ] **Step 4: 合成dumpで全工程を実行する**

```bash
bash -n ops/rehearsal.sh ops/rehearsal_test.sh
bash ops/rehearsal_test.sh
MTP_MANIFEST_SHA256="$MTP_APPROVED_MANIFEST_SHA256" MTP_RELEASE_RUN_ID="$MTP_APPROVED_RUN_ID" MTP_RELEASE_RUN_ATTEMPT="$MTP_APPROVED_RUN_ATTEMPT" MTP_REHEARSAL_DUMP="$MTP_SYNTHETIC_DUMP" MTP_REHEARSAL_DB_PASSWORD="$MTP_SYNTHETIC_DB_PASSWORD" bash ops/rehearsal.sh
```

Expected: 各工程pass、30分未満、既存project/volumeは未変更。

- [ ] **Step 5: orchestratorをコミットする**

```bash
git add ops/rehearsal.sh ops/rehearsal_test.sh
git commit -m "feat(migration): 移行リハーサルを自動化" -m "Refs #126"
```

### Task 5: 本番相当dumpとブラウザで合格判定する

**Files:**
- Create: `docs/deploy/go-migration-rehearsal.md`

- [ ] **Step 1: 機密data利用の承認を得る**

使用場所、閲覧者、暗号化・保管、ログに内容を出さないこと、既存backupを削除しないことを示し、オーナー承認後に人間が最新本番backupを隔離環境へ配置する。Codexは本番へ接続しない。

- [ ] **Step 2: runbookを完成させる**

manifest SHA256、commit SHA、release元workflow run URL/ID/attempt、rehearsal workflow run URL/ID、API完全digest、frontend/ops artifact名とSHA256、dump checksum、開始/終了時刻、件数/constraint/sequenceの検査可否、data digest一致可否と不一致table数、合成write期待差分/cleanup結果、承認範囲、smoke、ブラウザ操作、API p95、rollback所要時間、判定者を記録する。nonce、data digest自体、raw data、secretをIssueへ記録しない。#127が入力する合格証跡はmanifest SHA256、release元run ID/attempt、rehearsal run URL/IDを一組として示す。

- [ ] **Step 3: 人間が本番相当リハーサルを実行する**

manifestのfrontend artifactを配信したブラウザで既存利用者login/記事表示を確認し、データ比較後に合成利用者で新規登録、クリック、再読込、logoutを確認する。既存利用者の興味度を変える操作はしない。Swagger UIはtest環境で表示、本番相当production設定で404を確認する。停止相当工程と3成果物の組合せrollbackを各1回以上実測する。

- [ ] **Step 4: 合格判定をIssueへ記録する**

全自動検査pass、重大差分0、30分以内、rollback成功なら合格。1つでも満たさなければ #126をopenのままにし、#127を開始しない。

- [ ] **Step 5: 文書をコミットしてPRを作る**

```bash
git add docs/deploy/go-migration-rehearsal.md
git commit -m "docs(migration): Go移行リハーサル手順を記録" -m "Refs #126"
```

PRタイトルは `test(migration): Go移行と切り戻しを本番相当でリハーサルする`。合格証跡へのIssue linkと `Closes #126` を付け、人間がレビュー・マージする。
