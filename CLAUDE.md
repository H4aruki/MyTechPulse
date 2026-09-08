# CLAUDE.md

@AGENTS.md

Claude Codeでは、上の `AGENTS.md` をこのプロジェクトの共通ルールとして読み込んでください。

## Claude Code固有の補足

- 許可・禁止設定は `.claude/settings.json` にあります。
- 自動フックの動作と限界は `.claude/README.md` にあります。
- Skillsは `.claude/skills/` から読み込みます。内容は `.agents/skills/` と同一に保ちます。
- 共通ルールと機械的な防御を変更するときは、Claude Code側だけを変更せず、`AGENTS.md` と `.codex/` も確認します。
