# Claude CodeとCodexの対応

| 機能 | Claude Code | Codex |
|---|---|---|
| 共通指示 | `CLAUDE.md` から `@AGENTS.md` | `AGENTS.md` を直接探索 |
| ツール固有設定 | `.claude/settings.json` | `.codex/config.toml` |
| コマンド遮断 | `.claude/hooks/guard-command.mjs` | 同じ実装を `.codex/hooks.json` から呼ぶ |
| 繰り返し防止 | PreToolUseフック | PreToolUseフック |
| 検証漏れ防止 | PostToolUseとStopフック | PostToolUseとStopフック |
| コマンド規則 | permissionsとフック | `.codex/rules/default.rules` とフック |
| Skills | `.claude/skills/` | `.agents/skills/` |
| GitHub | 公式GitHubプラグイン | OpenAI curated GitHubプラグイン |
| Slack | 公式Slackプラグイン | OpenAI curated Slackプラグイン |
| Web検索 | Exaプラグイン | Exa MCP |
| ライブラリ資料 | Context7プラグイン | Context7 MCP |
| ブラウザ検証 | Playwrightプラグイン | Playwright MCPと組み込みブラウザ |
| 安全性確認 | Claude Security | Codex Security |
| 双方向worker起動 | `spawn-agent.mjs --activate multi --parent claude --agent codex` | `spawn-agent.mjs --activate multi --parent codex --agent claude` |
| workerの固定制約 | `sonnet`、`high`、専用worktree、再帰起動なし | `gpt-5.6-terra`、`high`、専用worktree、再帰起動なし |
| worker結果 | 共通JSON Schemaの `status`、`summary`、変更ファイル、テスト、課題、commit | 共通JSON Schemaの `status`、`summary`、変更ファイル、テスト、課題、commit |
| 起動前確認 | `spawn-agent.mjs --check` | `spawn-agent.mjs --check` |

完全に同じ実装ではありません。共通の目的と禁止事項を `AGENTS.md` に置き、各ツールの公式機構で同じ結果になるようにします。

workerの詳細な発動条件、レビュー、後片付けの扱いは [multi-agent.md](multi-agent.md) を正本とします。
