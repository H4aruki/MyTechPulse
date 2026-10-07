---
name: verify-change
description: Verifies MyTechPulse changes with the smallest relevant checks and honest result reporting. Use after editing code, configuration, hooks, or documentation, and before declaring work complete.
---

# 変更を検証する

1. `git diff --name-only` と `git diff --check` で変更範囲と書式を確認する。
2. 変更箇所に最も近い検証を先に実行する。
3. 完了前に、該当する全体検証を実行する。
   - バックエンド（`server/`）: `cd server && test -z "$(gofmt -l .)" && go vet ./... && go test ./... -race -cover`（DBが要る試験は `TEST_DATABASE_URL` が無いとスキップされる）
   - API契約・DB問い合わせを変えたとき: `cd server && go run ./cmd/openapi`（と `go tool sqlc generate`）で生成し直し、差分をコミットに含める
   - フロントエンド: `cd frontend && npm run lint && npm run test && npm run build`
   - 運用スクリプト（`ops/`）: 対応する `bash ops/<名前>_test.sh`
   - ハーネス・スクリプト: `node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs" "scripts/agent-harness/**/*.test.mjs" scripts/*.test.mjs`
4. 失敗時は、失敗したコマンド、要点、変更が原因か既存問題かを分けて報告する。
5. 実行していない検証を成功と書かない。機密ファイルの読み取り、本番接続、手動公開は検証目的でも行わない。
