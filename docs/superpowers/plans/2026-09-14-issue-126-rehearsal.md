# Issue 126 Migration Rehearsal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 本番切り替え前に、復元DBとrelease imageを使ってmigration、主要操作、性能、切り戻しを再現し、停止時間30分以内を実測する。

**Architecture:** 本番とは隔離したrehearsal Compose projectを使う。自動scriptは秘密値を出さず、件数・制約・health・所要時間を機械判定する。ブラウザ操作と本番相当バックアップの利用は人間が承認・実行し、結果だけをIssueへ記録する。

**Tech Stack:** Docker Compose、PostgreSQL 17、Bash、Go release image、React production build、GitHub Issues

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- 本番サーバー・本番DBへ接続しない
- rehearsalは既存Compose project、DB volume、portと別名を使う
- 本番由来dumpをrepositoryへ追加せず、内容・利用者名・password hashをログへ出さない
- 入力dumpと既存backupを削除・上書きしない
- migration後の件数、FK、unique、sequenceをmigration前と比較する
- release imageは #125 が作ったdigestを使い、rehearsal中にsource buildしない
- 合格条件は停止相当工程30分以内、主要flow全成功、rollback全成功、重大なdata差分0
- 合格しない状態で #127 を開始しない

---

## File Map

- Create: `docker-compose.rehearsal.yml` — 隔離DB/API/Caddy構成
- Create: `ops/rehearsal.sh`, `ops/rehearsal_test.sh` — 全工程orchestrator
- Create: `ops/sql/snapshot_migration_state.sql` — 秘密値なしの件数・制約snapshot
- Create: `ops/compare_migration_state.sh`, `compare_migration_state_test.sh` — 前後比較
- Create: `ops/rehearsal_smoke.sh`, `rehearsal_smoke_test.sh` — healthとAPI flow
- Create: `docs/deploy/go-migration-rehearsal.md` — 人間向けchecklist・記録欄

### Task 1: dataを露出しないmigration snapshotを作る

**Files:**
- Create: `ops/sql/snapshot_migration_state.sql`
- Create: `ops/compare_migration_state.sh`, `compare_migration_state_test.sh`

**Interfaces:**
- Produces: JSON `{schema_version, counts, constraints, sequences}`
- Consumes: migration前後のJSON file

- [ ] **Step 1: 比較scriptの失敗テストを書く**

同じuser/tag/recommend件数は成功、1件差は失敗。required FK/unique/check不足は失敗。各sequence `last_value < max_id` は失敗、password/tag名を含むkeyは失敗とする。

- [ ] **Step 2: snapshot SQLを書く**

SQLは3表の `count(*)`、PK/FK/unique/check制約名と定義、`user_user_ID_seq` と `tag_tag_ID_seq` のlast_value、各IDのmaxだけを `jsonb_build_object` で1行出力する。行内容、利用者名、password、tag名、session hashは選択しない。

- [ ] **Step 3: comparatorを実装する**

Nodeやjqを追加せず、PostgreSQLへ2 JSONを渡して `jsonb` 演算で比較するBash wrapperにする。migrationで意図的に増えるrole/auth_session制約はafter必須、3表件数は完全一致、auth_session件数は比較対象外とする。

- [ ] **Step 4: 合成fixtureで検証してコミットする**

```bash
bash -n ops/compare_migration_state.sh ops/compare_migration_state_test.sh
bash ops/compare_migration_state_test.sh
git add ops/sql/snapshot_migration_state.sql ops/compare_migration_state.sh ops/compare_migration_state_test.sh
git commit -m "test(migration): DB前後状態の比較を追加" -m "Refs #126"
```

### Task 2: 隔離rehearsal環境を作る

**Files:**
- Create: `docker-compose.rehearsal.yml`

**Interfaces:**
- Consumes: `MTP_REHEARSAL_IMAGE`、`MTP_REHEARSAL_DUMP`、合成password
- Produces: project `mytechpulse-rehearsal`、loopback ports 18001/15432

- [ ] **Step 1: Compose設定testを書く**

```bash
MTP_REHEARSAL_IMAGE='ghcr.io/h4aruki/mytechpulse-api-go@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' \
MTP_REHEARSAL_DB_PASSWORD='synthetic-rehearsal-only' \
docker compose -p mytechpulse-rehearsal -f docker-compose.rehearsal.yml config
```

出力にproduction volume名、port 8000/8001/5432、build節が無いことを文字列検査する。

- [ ] **Step 2: 隔離構成を書く**

DBはnamed volume `mytechpulse_rehearsal_db`、host bind `127.0.0.1:15432`。APIは `MTP_REHEARSAL_IMAGE` 必須、host bind `127.0.0.1:18001`、APP_ENV test、Swagger有効。migrationは同じdigestのimageで `/migrate` entrypointを一度実行する。

- [ ] **Step 3: image値をdigest限定にする**

Compose実行前のscriptで `^ghcr\.io/h4aruki/mytechpulse-api-go@sha256:[0-9a-f]{64}$` を検証する。latestやcommit tagだけでは起動しない。

- [ ] **Step 4: config検証してコミットする**

```bash
docker compose -p mytechpulse-rehearsal -f docker-compose.rehearsal.yml config
git add docker-compose.rehearsal.yml
git commit -m "feat(migration): 隔離リハーサル環境を追加" -m "Refs #126"
```

### Task 3: 認証・feed・clickのsmoke testを作る

**Files:**
- Create: `ops/rehearsal_smoke.sh`, `rehearsal_smoke_test.sh`

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

dump不存在、checksum不一致、image digest不正は起動前に失敗。restore/migrate/smoke/comparisonのどれかが失敗したら後続切り替えをせず非0。Python旧APIのhealthへ戻すrollback模擬が成功したときだけrollback passとする。

- [ ] **Step 2: 工程を実装する**

開始epochを記録し、backup検証→隔離DB起動→新規DBへrestore→before snapshot→migration→after snapshot→比較→Go起動→smoke→旧Python imageへroutingを戻す模擬→health→Goへ再切替→healthの順にする。各工程は名前・秒数・pass/failだけを出す。

- [ ] **Step 3: 終了処理を安全にする**

自動終了はcontainer stopだけを行い、input dumpとnamed volumeは保持する。実行時に作った一時Cookie/snapshot fileだけをtrapで削除する。volumeを削除する `down -v` はscriptへ入れない。

- [ ] **Step 4: 合成dumpで全工程を実行する**

```bash
bash -n ops/rehearsal.sh ops/rehearsal_test.sh
bash ops/rehearsal_test.sh
MTP_REHEARSAL_IMAGE="$MTP_RELEASE_IMAGE" MTP_REHEARSAL_DUMP="$MTP_SYNTHETIC_DUMP" MTP_REHEARSAL_DB_PASSWORD="$MTP_SYNTHETIC_DB_PASSWORD" bash ops/rehearsal.sh
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

digest、dump checksum、開始/終了時刻、migration前後件数、constraint、sequence、smoke、ブラウザ操作、API p95、rollback所要時間、判定者を記録する表を作る。値欄はIssue実行コメントへ記録し、機密値は書かない。

- [ ] **Step 3: 人間が本番相当リハーサルを実行する**

ブラウザで既存利用者login、記事表示、クリック、再読込、logout、新規登録を確認する。Swagger UIはtest環境で表示、本番相当production設定で404を確認する。停止相当工程とrollbackを各1回以上実測する。

- [ ] **Step 4: 合格判定をIssueへ記録する**

全自動検査pass、重大差分0、30分以内、rollback成功なら合格。1つでも満たさなければ #126をopenのままにし、#127を開始しない。

- [ ] **Step 5: 文書をコミットしてPRを作る**

```bash
git add docs/deploy/go-migration-rehearsal.md
git commit -m "docs(migration): Go移行リハーサル手順を記録" -m "Refs #126"
```

PRタイトルは `test(migration): Go移行と切り戻しを本番相当でリハーサルする`。合格証跡へのIssue linkと `Closes #126` を付け、人間がレビュー・マージする。
