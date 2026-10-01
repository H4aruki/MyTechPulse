# ハーネス変更履歴

## 2026-10-01

- 同じツールを内部サブエージェントとして呼ぶときの既定を追加。Claude Codeは `sonnet` / `high`、Codexは `gpt-6-luna` / `high`（指定した段階で固定。自動で下げる仕組みはない）
- `/multi` のランナーが起動するCodex workerも `gpt-6-luna` / `high` に変更（Claude Code workerは `sonnet` / `high` のまま）

## 2026-09-28

- `/multi`、`/parallel`、または明確な並列委譲がある場合だけ、CodexとClaude Codeが共通ランナーからworkerを起動する運用を追加
- Codex workerを `gpt-5.6-terra` / `high`、Claude Code workerを `sonnet` / `high` に固定し、親のモデル・推論量を変えない方針を明記
- 専用worktree、構造化結果、親による差分レビューと、人が行う統合判断を共通文書へ追加

## 2026-09-09

- `AGENTS.md` をClaude CodeとCodexの共通ルール正本として追加
- Claude CodeとCodexで共用する安全フックを追加
- Codexのサンドボックス設定とコマンド規則を追加
- 公式3種類とプロジェクト固有11種類のSkillsを両ツールへ追加
- Skillsの内容差を検出する自動テストを追加
- GitHub、Slack、Security、Exa、Context7、Playwrightを標準連携として整理
