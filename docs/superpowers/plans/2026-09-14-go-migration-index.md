# Go Migration Execution Index Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Issue #118〜#129を、既存利用者データを保持したまま安全な依存順で完了させる。

**Architecture:** Go版は `server/` にモジュラーモノリスとして並行構築し、Python版 `backend/` は切り戻し期間中維持する。各Issueは独立した計画とPRを持ち、安全網、基盤、業務機能、接続、リハーサル、本番切り替え、整理の順で進める。

**Tech Stack:** Go 1.26、Huma v2、pgx v5、sqlc、goose、PostgreSQL 17、React、TypeScript、Vitest、Docker Compose、GitHub Actions

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- `.env` と `backend/.env` の値を読まない。設定項目は `.env.example` だけで確認する
- 本番サーバーへ直接接続しない。本番公開は承認済みGitHub Actionsだけで行う
- 既存利用者データ、DBボリューム、既存ファイルを削除しない。削除は対象・役割・理由・影響を示して個別承認を得る
- Python版 `backend/` は #127の切り替えと安定確認が終わるまで維持する
- Zennは `https://zenn.dev/api/articles` を使い、RSSへ自動切り替えしない
- 興味度はDBの `recommend.match_int` に10000倍整数で保存する
- GoコードをOpenAPIの正本とし、本番のSwagger UIは既定で無効にする
- 各PRは人間がレビュー・承認・マージする。Codexはマージしない

---

## 実行順

| 順序 | Issue | 計画 | 開始条件 |
| --- | --- | --- | --- |
| 1A | #118 | [現行仕様の固定](2026-09-14-issue-118-characterization.md) | 本設計の承認 |
| 1B前半 | #119 | [DB変更と復元](2026-09-14-issue-119-database-safety.md) | 本設計の承認。スキーマ監査とバックアップ準備だけ先行 |
| 2 | #120 | [Go共通基盤](2026-09-14-issue-120-go-foundation.md) | #117、#119のDB方針 |
| 3A | #119後半 | [DB変更と復元](2026-09-14-issue-119-database-safety.md) | #120のGo・goose基盤 |
| 3B | #121 | [認証](2026-09-14-issue-121-auth.md) | #118〜#120 |
| 3C | #122 | [外部記事](2026-09-14-issue-122-providers.md) | #118、#120 |
| 3D | #123 | [興味と推薦](2026-09-14-issue-123-recommendation.md) | #118〜#122 |
| 4A | #125前半 | [CIと配布](2026-09-14-issue-125-ci-deployment.md) | #120後。旧frontend/backendの自動公開を先に凍結 |
| 4B | #124 | [フロント接続](2026-09-14-issue-124-frontend.md) | #121〜#123、#125の旧公開凍結 |
| 4C | #125後半 | [CIと配布](2026-09-14-issue-125-ci-deployment.md) | #124後に生成検査とrelease imageを完成 |
| 5 | #126 | [移行リハーサル](2026-09-14-issue-126-rehearsal.md) | #119、#121〜#125 |
| 6 | #127 | [本番切り替え](2026-09-14-issue-127-production-cutover.md) | #126成功と本番操作の明示承認 |
| 7 | #128 | [旧構成整理](2026-09-14-issue-128-cleanup.md) | #127後の安定確認と削除の明示承認 |
| 8 | #129 | [文書統一](2026-09-14-issue-129-documentation.md) | #128完了 |

同じ段階のIssueを並行実行する場合も、共通ファイルは1つのPRだけが変更する。#121と#122はmodule内だけを並行実装し、#123が `server/internal/app/`、`cmd/api`、生成OpenAPIへ認証・provider・推薦を順に統合する。#124は確定したOpenAPIを使ってfrontendだけを接続する。

## 設計書の網羅表

| 設計節 | 実装・確認Issue |
| --- | --- |
| 背景・目標・非対象・採用方式 | #117、全IssueのGlobal Constraints |
| repository構成・module境界 | #120、#121、#122、#123 |
| API・Swagger・error | #120、#121、#123、#124 |
| 認証・認可 | #121、#124 |
| DB変更・data保全 | #118、#119、#121、#123 |
| 記事取得・推薦 | #118、#122、#123 |
| 設定・log・終了処理 | #120、#122、#123 |
| OpenAPI・frontend | #120、#121、#123、#124 |
| test戦略 | #118〜#126 |
| CI・成果物・公開 | #125、#127 |
| 切り替え・切り戻し | #126、#127、#128 |
| 文書・最終完了判定 | #129、親 #116 |

## 実行時に必要な明示承認

| 段階 | 承認対象 | 承認前に行わないこと |
| --- | --- | --- |
| #119 | 本番外backup保存先、公開範囲、費用、保存期間 | 外部保存、世代削除 |
| #125 | GHCR packageの公開範囲、認証、費用 | image push、package設定変更 |
| #126 | 本番由来backupを隔離環境で扱う範囲 | backup取得・搬送・内容利用 |
| #127 | 本番API/画面/DB migrationと5〜30分停止 | production workflow実行 |
| #128 | tracked Python filesと依存の正確な一覧 | `git rm` とruntime参照除去 |
| #129 | 別AI tool用skillの読取・変更 | Codexによる当該fileの参照・同期 |

各Issueは、計画に書いた自動検査、必要な人間確認、PRのレビュー・マージ、Issueの受入条件をすべて満たして初めてcloseする。途中でPRだけがmergeされても、運用確認が残るIssueはopenを維持する。
