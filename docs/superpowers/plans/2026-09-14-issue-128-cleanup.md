# Issue 128 Legacy Python Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go版の安定確認後、切り戻しに必要な証跡を保ったまま、Python版の追跡対象コード・依存・CI・稼働経路を明示許可後に整理する。

**Architecture:** 7日間の安定条件を先に判定し、参照監査から正確な削除候補を生成する。削除対象と影響をユーザーへ提示し、承認されたtracked fileだけを削除する。DB・backup・旧image・local秘密fileは対象外にする。

**Tech Stack:** Git、GitHub Actions、Docker Compose、Go 1.26、React、repository documentation

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- #127成功から連続7日以上経過するまで開始しない
- 期間中にdata欠損、認証不能、重大security問題、主要flow停止が1件でもあれば日数を0から数え直す
- 既存file/data削除前に対象、役割、理由、影響を提示し、ユーザーの明示許可を得る
- `backend/.env`、root `.env`、DB volume、backup、旧image、旧Cloudflare deploymentを削除しない
- untracked/ignored fileは削除しない
- `git clean`、`git reset --hard`、volume prune、image pruneを使わない
- Go本番とrollback証跡が正常でない状態でPython経路を外さない
- 過去履歴はGitに残し、squashや履歴書換えをしない

---

## File Map

- Create: `ops/check_go_stability.sh`, `check_go_stability_test.sh` — 7日条件
- Create: `docs/deploy/python-retirement-record.md` — 許可・保持物・結果
- Delete after approval: tracked `backend/` files — Python API、test、Dockerfile
- Delete after approval: `requirements.txt`, `requirements-dev.txt` — Python依存
- Modify: `docker-compose.yml` — Python `api` serviceと参照を除去
- Modify: `Caddyfile` — default upstreamをGoへ確定
- Modify: `.github/workflows/ci.yml`, `release.yml` — Python job/rollback分岐を除去
- Modify: `.gitignore`, `.dockerignore` — Python専用entryを除去
- Modify: `dev.ps1` — Go起動へ変更
- Modify: `ops/rollback_release.sh` — Python復帰を終了し、直前Go digest復帰へ変更
- Create: `scripts/check-no-python-runtime.mjs` — 現行runtimeのPython参照検査

### Task 1: 7日間の安定条件を機械判定する

**Files:**
- Create: `ops/check_go_stability.sh`, `check_go_stability_test.sh`

**Interfaces:**
- Consumes: #127完了日時、日別health、重大incident件数、最新backup復元結果
- Produces: cleanup開始可否

- [ ] **Step 1: 境界testを書く**

6日23時間は拒否、7日ちょうどで許可。途中incident、日別health欠落、restore未成功、旧image digest不明は拒否する。値は合成JSON fixtureから読む。

- [ ] **Step 2: checkerを実装する**

UTC時刻で経過を計算し、7つの連続日すべてにready成功があり、重大incident=0、期間中に1回以上backup復元成功、旧Python image digest記録ありを要求する。利用者情報やログ本文は入力にしない。

- [ ] **Step 3: testしてコミットする**

```bash
bash -n ops/check_go_stability.sh ops/check_go_stability_test.sh
bash ops/check_go_stability_test.sh
git add ops/check_go_stability.sh ops/check_go_stability_test.sh
git commit -m "test(migration): Go版の安定確認条件を固定" -m "Refs #128"
```

### Task 2: 削除候補と参照をread-only監査する

**Files:**
- Create: `docs/deploy/python-retirement-record.md`

- [ ] **Step 1: tracked候補を正確に取得する**

```bash
git ls-files backend requirements.txt requirements-dev.txt
git status --short --branch
```

出力を削除候補一覧へ転記する。`backend/.env` は存在確認も内容確認もせず対象外とし、untracked/ignored fileは一覧へ加えない。

- [ ] **Step 2: repository参照を監査する**

```bash
rg -n "backend/|requirements(-dev)?\.txt|python|pytest|ruff|api:8000|postgresql\+psycopg" -g '!backend/**' -g '!docs/superpowers/**' -g '!.git/**' -g '!.claude/**'
```

各参照を「実行経路」「現行文書」「履歴/移行記録」に分類する。履歴/移行記録は事実として残す。

- [ ] **Step 3: runtime保持物を記録する**

旧Python image digest、最後に成功したcommit、直前frontend deployment ID、backup checksum、rollback手順のGit commitを記録する。secret、利用者名、DB内容は書かない。

- [ ] **Step 4: 記録をコミットする**

```bash
git add docs/deploy/python-retirement-record.md
git commit -m "docs(migration): Python版整理の対象と保持物を記録" -m "Refs #128"
```

### Task 3: 削除許可を得る

- [ ] **Step 1: 4点をユーザーへ提示する**

対象はTask 2の `git ls-files` で確定したtracked `backend/` filesとroot `requirements.txt`、`requirements-dev.txt`。概要は旧FastAPI本体・test・build定義・依存。理由はGo本番安定後の二重保守解消。影響はPythonを現在のcheckoutから起動できなくなるが、Git履歴・旧image・DB・backup・local秘密fileは残る、と説明する。

- [ ] **Step 2: 明示回答を待つ**

許可されなければ削除を実行せず、#128をopenのままにする。対象追加を推測しない。

### Task 4: 承認されたtracked Python filesだけを削除する

**Files:**
- Delete after approval: Task 2で確定しTask 3で承認されたfile

- [ ] **Step 1: 承認一覧と現在のtracked一覧を照合する**

```bash
git ls-files backend requirements.txt requirements-dev.txt
git status --short --branch
```

一覧が承認時から変わっていれば削除せず、差分を再提示する。

- [ ] **Step 2: tracked pathだけを `git rm` する**

`git rm` には承認済みfileを個別に渡す。directory wildcard、`git clean`、PowerShell再帰削除を使わない。ignored `backend/.env` やvenvが残っても削除しない。

- [ ] **Step 3: 削除結果を検査する**

```bash
git status --short
git diff --stat
git diff --name-status
```

Expected: 承認済みtracked fileだけが `D`。DB、backup、imageへの操作なし。

- [ ] **Step 4: 削除をコミットする**

```bash
git commit -m "refactor(backend): 安定確認済みPython実装を整理" -m "Refs #128"
```

### Task 5: 稼働経路とCIをGoだけへ整理する

**Files:**
- Modify: `docker-compose.yml`, `Caddyfile`
- Modify: `.github/workflows/ci.yml`, `release.yml`
- Modify: `.gitignore`, `.dockerignore`, `dev.ps1`
- Modify: `ops/rollback_release.sh`, `rollback_release_test.sh`

- [ ] **Step 1: Python参照が残る失敗testを書く**

runtime対象fileに `backend/`、requirements、pytest、ruff、`api:8000`、Python service名があれば失敗するscannerを `scripts/check-no-python-runtime.mjs` として作り、docs/superpowersとretirement recordは対象外にする。

- [ ] **Step 2: Compose/Caddy/devをGoへ確定する**

service名を `api` にrenameせず `api-go` を維持し、Caddy defaultを `api-go:8001` とする。Composeの旧 `api` blockと依存だけを除き、DB volume定義は変更しない。dev.ps1はDB→migrate→Go API→frontendを案内する。

- [ ] **Step 3: CI/releaseからPythonを外す**

Python setup、ruff、pytest、source build deployを除く。Go、migration、OpenAPI、frontend、Docker、action pinの必須jobを維持する。必須check変更はrepository rulesetと同じPRで人間が確認する。

- [ ] **Step 4: rollbackを直前Go digestへ変更する**

Python復帰branchを除き、直前に成功したGo digestと対応Cloudflare deploymentへ戻す。DB downは使わず、image/backupを削除しない。

- [ ] **Step 5: 全検証してコミットする**

```bash
node scripts/check-no-python-runtime.mjs
node scripts/verify-actions-pinned.mjs
bash ops/rollback_release_test.sh
cd server
go vet ./...
go test ./... -race
go build ./cmd/api ./cmd/migrate ./cmd/openapi
cd ../frontend
npm run api:check
npm run lint
npm run test
npm run build
cd ..
docker compose config
git diff --check
git add docker-compose.yml Caddyfile .github/workflows .gitignore .dockerignore dev.ps1 ops/rollback_release.sh ops/rollback_release_test.sh scripts/check-no-python-runtime.mjs
git commit -m "refactor(backend): 稼働経路をGoへ統一" -m "Refs #128"
```

### Task 6: PRと削除後確認を行う

- [ ] **Step 1: PRを作る**

PRタイトルは `refactor(backend): 安定確認後にPython版の稼働経路を整理する`。承認された削除一覧、7日証跡、保持したDB/backup/image、Go/rollback検証を記載し、`Closes #128` を付ける。

- [ ] **Step 2: 人間のレビュー後に状態を確認する**

Codexはマージしない。マージ後もGo ready、既存利用者login、feed、click、logout、backup jobを確認する。削除したtracked fileはGit履歴から復元可能、local ignored fileは未操作と最終報告する。
