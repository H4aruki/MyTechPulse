# Issue 129 Documentation Consolidation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Go移行後のコード・OpenAPI・DB・運用を正本にして全現行資料を更新し、新しいメンバーが開発から切り戻しまで再現できる状態にする。

**Architecture:** OpenAPI、migration、Compose、CIを機械確認できる正本、ADRを設計判断の正本、README/基本設計/運用資料を人間向け入口とする。文書間linkと旧Python/JWT契約の現行表記を自動検査する。

**Tech Stack:** Markdown、Mermaid、Node.js標準API、OpenAPI 3.1、GitHub Issues

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- 実装・OpenAPI・migrationの確認前に文書を推測で確定しない
- 過去の移行設計、ADR、retirement recordは履歴として残し、現行仕様と明示する
- `.env` と `backend/.env` の値を読まず、例示に実secretを使わない
- 他のAI tool用設定をCodexから自発的に読まない・変更しない
- `.agents/skills` 変更で別tool側の同期が必要なら、対象と理由を示して明示許可を得るか、ユーザーが手動同期する
- obsolete文書を削除せず、現行でない場合は冒頭へ履歴表示と現行linkを追加する
- Issueは受入条件と証跡が満たされたものだけcloseする

---

## File Map

- Modify: `README.md`, `frontend/README.md` — 開発・構成・Swagger入口
- Modify: `AGENTS.md`, `CONTRIBUTING.md` — Go構成・検証・境界
- Modify: `.env.example`, `server/.env.example`, `frontend/.env.example` — 項目説明
- Modify: `CONTEXT.md` — 実装で確定した用語差分
- Modify: `docs/RequirementsSpecification.md` — Cookie認証とGo構成
- Modify: `docs/BasicDesignSpecifications/SystemArchitectureDiagram.md` — 配置とmodule境界
- Modify: `docs/BasicDesignSpecifications/DataBaseArchitecture.md` — role/session/migration
- Modify: `docs/BasicDesignSpecifications/FeaturesList.md` — 実装状態
- Modify: `docs/BasicDesignSpecifications/API/ApiList.md`, `ApiCommonRules.md`, `ApiExternal.md` — `/api/v1`・Problem Details・提供元
- Modify: `docs/BasicDesignSpecifications/API/Details/Auth.md`, `News.md`, `Article.md` — 正確な契約
- Modify: `docs/BasicDesignSpecifications/Screen/ScreenList.md`, `ScreenCommonRules.md`
- Modify: `docs/BasicDesignSpecifications/Screen/Details/SignUp.md`, `Login.md`, `ArticleList.md`
- Modify: `docs/deploy/lightsail-provisioning.md`, `oracle-vm-provisioning.md` — immutable image運用
- Modify: `docs/DocumentMap.md`, `TASKS.md` — 資料入口と残作業
- Modify: `.agents/skills/mytechpulse-domain/SKILL.md` — Qiita/Zenn差分を正確化
- Create: `scripts/check-doc-links.mjs`, `check-doc-links.test.mjs` — link検査
- Create: `scripts/check-current-docs.mjs`, `check-current-docs.test.mjs` — 旧現行表記検出

### Task 1: 実装から現行値をread-onlyで採取する

**Files:**
- Create: `docs/deploy/documentation-evidence.md`

- [ ] **Step 1: 正本を機械出力する**

```bash
cd server
go run ./cmd/openapi
go tool sqlc generate
go test ./db/migrations -run TestLatestVersion -v
go list -m all
cd ..
docker compose config
node scripts/verify-actions-pinned.mjs
```

OpenAPI path/status/schema、migration version、Go module、Compose service、CI job名だけを記録する。接続URLや環境変数値は記録しない。

- [ ] **Step 2: domain値をtestから確認する**

```bash
cd server
go test ./internal/interest ./internal/recommendation ./internal/provider/... -v
cd ../frontend
npm run api:check
npm run test
```

Qiita 5日・likes倍率、Zenn 14日・likes非倍率・現行API・期間fallback、上位5タグ、提供元別10件、クリック8/10+2000をevidenceへ記録する。

- [ ] **Step 3: 運用結果をIssueから確認する**

#119 backup/restore、#125 image digest、#126 rehearsal、#127切替、#128安定・整理の証跡URLと結果だけを記録する。個人data、secret、外部応答bodyは転記しない。

- [ ] **Step 4: evidenceをコミットする**

```bash
git add docs/deploy/documentation-evidence.md
git commit -m "docs(migration): 現行仕様の根拠を記録" -m "Refs #129"
```

### Task 2: 開発者入口と環境構築をGoへ更新する

**Files:**
- Modify: `README.md`, `frontend/README.md`, `AGENTS.md`, `CONTRIBUTING.md`
- Modify: `.env.example`, `server/.env.example`, `frontend/.env.example`

- [ ] **Step 1: READMEへ最短開始手順を書く**

前提Go 1.26/Node 22/PostgreSQL 17、DB起動、migration、Go API、frontend、test、Swagger `http://127.0.0.1:8001/docs` の順を記載する。本番Swaggerは無効と明記する。

- [ ] **Step 2: AGENTS/CONTRIBUTINGを実構成へ合わせる**

Pythonのroutes→services→crud→modelsとpytest/ruffを現行説明から外し、`server/cmd`、`internal/auth|article|recommendation|feed|provider|store|platform`、go test/vet/build、OpenAPI/sqlc差分を記載する。

パーソナライズ要約は次へ正す。

```text
Qiita: 直近5日、一致興味度合計×(likes+1)
Zenn: 直近14日、一致興味度合計。期間内0件は取得済み候補から期間だけ外す
共通: 興味度上位5タグ、URL重複排除、提供元別上位10件
```

- [ ] **Step 3: example envを実Configと照合する**

値はlocal合成例だけにし、APP_ENV、HTTP_ADDR、DATABASE_URL、QIITA_ACCESS_TOKEN、CORS_ALLOWED_ORIGINS、SESSION_TTL、PROVIDER_TIMEOUT、FEED_TIMEOUT、PROVIDER_MAX_BYTES、SWAGGER_ENABLEDを説明する。production用secret値は置かない。

- [ ] **Step 4: 入口文書をコミットする**

```bash
git add README.md frontend/README.md AGENTS.md CONTRIBUTING.md .env.example server/.env.example frontend/.env.example
git commit -m "docs(dev): Go版の開発入口へ更新" -m "Refs #129"
```

### Task 3: API・DB・画面の基本設計を現行契約へ更新する

**Files:**
- Modify: `docs/RequirementsSpecification.md`
- Modify: `docs/BasicDesignSpecifications/SystemArchitectureDiagram.md`
- Modify: `docs/BasicDesignSpecifications/DataBaseArchitecture.md`
- Modify: `docs/BasicDesignSpecifications/FeaturesList.md`
- Modify: `docs/BasicDesignSpecifications/API/*`
- Modify: `docs/BasicDesignSpecifications/Screen/*`

- [ ] **Step 1: API文書をOpenAPIへ合わせる**

6つの `/api/v1` 経路と2つのhealth経路のmethod/path/status、Problem Details、Cookie、CORS/CSRF、再login、feedの `qiita_articles`/`zenn_articles`/`warnings`、clickの204を記載する。旧 `status` 数値、Bearer JWT、旧pathは「移行前」と明示した履歴節以外から除く。

- [ ] **Step 2: 外部APIと推薦値を正す**

Qiitaは `/api/v2/tags/{tag}/items`、5日、likes倍率。Zennは `https://zenn.dev/api/articles`、`topicname` 小文字、`count=5`、14日、likesは表示のみ、期間fallback。timeout 5秒、全体8秒、body 2MiB、partial successを記載する。

- [ ] **Step 3: DB文書をmigrationへ合わせる**

既存3表にrole列、auth_session表、PK/FK/index/check、固定小数点、goose適用経路を加える。記事非保存を維持し、ER図を更新する。

- [ ] **Step 4: 画面文書をCookie sessionへ合わせる**

ProtectedRouteの `/me` 判定、HTTP status別表示、部分成功warning、server logout、再loginを記載する。LPや画面見た目の変更は加えない。

- [ ] **Step 5: 基本設計をコミットする**

```bash
git add docs/RequirementsSpecification.md docs/BasicDesignSpecifications
git commit -m "docs(design): API・DB・画面をGo版へ更新" -m "Refs #129"
```

### Task 4: 運用資料と文書mapを統一する

**Files:**
- Modify: `docs/deploy/lightsail-provisioning.md`, `oracle-vm-provisioning.md`
- Modify: `docs/DocumentMap.md`, `TASKS.md`, `CONTEXT.md`

- [ ] **Step 1: provisioningをimage pull方式へ更新する**

GHCR認証、digest指定、migration、Caddy upstream、health、本番Swagger無効、backup/restore、rollbackを記載する。source buildとPython起動は移行履歴として分離する。実host、token、passwordを書かない。

- [ ] **Step 2: DocumentMapへ全現行資料を追加する**

ADR、移行spec、plan index、DB backup/restore、CI/release、rehearsal、cutover、retirement record、documentation evidenceをtreeと一覧へ追加する。各説明は1〜2文にする。

- [ ] **Step 3: CONTEXT/TASKSを実装結果へ合わせる**

用語はOpenAPI/コードと一致させる。完了Issueは完了欄へ移し、未実装の管理者画面・定期収集・AI連携は将来候補として残す。今回の移行で実装したとは書かない。

- [ ] **Step 4: 運用文書をコミットする**

```bash
git add docs/deploy docs/DocumentMap.md TASKS.md CONTEXT.md
git commit -m "docs(ops): Go版の運用資料と文書mapを統一" -m "Refs #129"
```

### Task 5: Codex用domain skillを正し、他tool設定は許可制で扱う

**Files:**
- Modify: `.agents/skills/mytechpulse-domain/SKILL.md`

- [ ] **Step 1: `writing-skills` と `harness-maintenance` を読む**

skill本文と共有harnessを変更するため、実装turnで両skillを先に適用する。

- [ ] **Step 2: `.agents` 側だけ変更案を作る**

AGENTSと同じQiita/Zenn差、固定小数点、現行Zenn API、部分成功を記載する。Go module境界を使い、旧Python layer名を現行指示から外す。

- [ ] **Step 3: 別AI tool側の同期方法をユーザーへ相談する**

parity check上、対応する別tool用skillも同内容にする必要がある。Codexがそのfileを読んで変更する案は、対象・理由を提示して明示許可を得る。許可されない場合は、`.agents` の確定diffをユーザーが別tool側へ手動同期し、そのcommit後にCodexは通常CI結果だけを確認する。

- [ ] **Step 4: 許可範囲内で検証してコミットする**

```bash
node --test scripts/agent-harness/check-skill-parity.test.mjs
node scripts/agent-harness/check-skill-parity.mjs
git add .agents/skills/mytechpulse-domain/SKILL.md
git commit -m "docs(domain): 提供元別の推薦仕様を正確化" -m "Refs #129"
```

Codexが別tool用設定をstage対象へ加えるのは、ユーザーがそのfileの読取・変更を明示許可した場合だけとする。

### Task 6: 文書linkと旧現行表記を自動検査する

**Files:**
- Create: `scripts/check-doc-links.mjs`, `check-doc-links.test.mjs`
- Create: `scripts/check-current-docs.mjs`, `check-current-docs.test.mjs`

- [ ] **Step 1: Markdown link checkerのtestを書く**

relative file/anchorの存在、URLと画像linkの除外、空白を含むpathのdecodeを合成directoryで確認する。壊れたlinkはfile:lineとlink先だけを出す。

- [ ] **Step 2: current-doc scannerのtestを書く**

README、AGENTS、CONTRIBUTING、Requirements、BasicDesign、deploy、frontend READMEを対象に、現行説明中の旧path、JWT/localStorage/status契約、Zenn 5日/likes倍率、Python起動を検出する。`docs/superpowers`、ADRのcontext、retirement record、明示的な「移行前」節は除外する。

- [ ] **Step 3: Node標準APIだけで実装する**

追加npm packageは使わない。検査対象はcode内の固定allowlistとし、`.env`、別tool用設定、Git objectを探索しない。

- [ ] **Step 4: 全文書検査を実行する**

```bash
node --test scripts/check-doc-links.test.mjs scripts/check-current-docs.test.mjs
node scripts/check-doc-links.mjs
node scripts/check-current-docs.mjs
git diff --check
```

- [ ] **Step 5: checkerをコミットする**

```bash
git add scripts/check-doc-links.mjs scripts/check-doc-links.test.mjs scripts/check-current-docs.mjs scripts/check-current-docs.test.mjs
git commit -m "test(docs): 文書linkと旧仕様表記を検査" -m "Refs #129"
```

### Task 7: 全Issueと最終ゴールを監査する

- [ ] **Step 1: 全検証を再実行する**

```bash
cd server
go vet ./...
go test ./... -race
go build ./cmd/api ./cmd/migrate ./cmd/openapi
go run ./cmd/openapi
git diff --exit-code -- internal/store/dbgen openapi/openapi.json
cd ../frontend
npm run api:check
npm run lint
npm run test
npm run build
cd ..
node scripts/verify-actions-pinned.mjs
node scripts/check-doc-links.mjs
node scripts/check-current-docs.mjs
docker compose config
git diff --check
```

- [ ] **Step 2: 新規Issue #117〜#129を1件ずつreadbackする**

各受入条件、PR、test、本番・data証跡を対応表にする。未達が1つでもあればそのIssueと親 #116をopenにし、完了と報告しない。

- [ ] **Step 3: 既存関連Issueを重複監査する**

#14、#46、#55、#60、#103、#110を最新bodyと実装で確認し、今回の変更で完全に満たしたものだけ証跡コメント後にcloseする。部分的なら関連PR/Issue linkを追記してopenを維持する。

- [ ] **Step 4: PRを作る**

PRタイトルは `docs: Go移行後の設計・開発・運用資料を統一する`。更新資料、正本、検査結果、他tool設定の扱い、関連Issue監査結果を記載し、`Closes #129` を付ける。人間がレビュー・マージする。

- [ ] **Step 5: 親Issueを完了する**

人間のマージ後、#117〜#129がすべてclosed、既存利用者data一致、主要flow成功、Swagger/OpenAPI一致、backup/rollback検証済みであることを再確認する。証跡表を #116へコメントし、#116をcloseして今回のゴール完了とする。
