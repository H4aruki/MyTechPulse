# CLAUDE.md

@AGENTS.md

Claude Codeでは、上の `AGENTS.md` をこのプロジェクトの共通ルールとして読み込んでください。

## Claude Code固有の補足

- 許可・禁止設定は `.claude/settings.json` にあります。
- 自動フックの動作と限界は `.claude/README.md` にあります。
- Skillsは `.claude/skills/` から読み込みます。内容は `.agents/skills/` と同一に保ちます。
- 共通ルールと機械的な防御を変更するときは、Claude Code側だけを変更せず、`AGENTS.md` と `.codex/` も確認します。

## 双方向worker

双方向workerの共通規則は [AGENTS.md](AGENTS.md)、詳しい運用手順は
[docs/agent-harness/multi-agent.md](docs/agent-harness/multi-agent.md) を参照します。
Claude Codeを親にしてCodex workerを起動する明示依頼だけ、次の共通ランナーを使います。

```powershell
node scripts/agent-harness/spawn-agent.mjs --activate multi --parent claude --agent codex --worktree auth-api --task "server/internal/auth/配下の認証APIだけを担当し、テストとコミットを行う"
```
