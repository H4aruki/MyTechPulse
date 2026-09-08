---
name: harness-maintenance
description: Maintains the shared Codex and Claude Code harness without configuration drift. Use when changing AGENTS.md, CLAUDE.md, hooks, rules, skills, MCP guidance, or harness CI.
---

# ハーネスを保守する

1. 共通方針の正本は `AGENTS.md`、Claude固有入口は `CLAUDE.md`、Codex固有設定は `.codex/` とする。
2. 禁止事項を変える場合は、文章、Claude設定・フック、Codex規則・フック、テストを同時に確認する。
3. Skillを追加・更新する場合は `.agents/skills/` と `.claude/skills/` の同名Skillを同一内容にする。
4. 利用者固有の認証情報やローカル設定をコミットしない。
5. `node scripts/agent-harness/check-skill-parity.mjs` とフックテストを実行する。
6. 外部仕様に依存する変更は、CodexとClaude Codeの公式資料で現在の形式を確認する。
