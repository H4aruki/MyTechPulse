# エージェント・ハーネス

MyTechPulseでClaude CodeとCodexを同じ方針で安全に使うための構成です。

## 正本と役割

| 対象 | 正本 | 役割 |
|---|---|---|
| 共通ルール | `AGENTS.md` | 作業手順、禁止事項、プロジェクト仕様 |
| Claude Code入口 | `CLAUDE.md` | `AGENTS.md` の読み込みとClaude固有案内 |
| Claude Code設定 | `.claude/` | 許可、フック、Skills |
| Codex設定 | `.codex/` | サンドボックス、フック、コマンド規則 |
| Codex Skills | `.agents/skills/` | Codexが使う反復可能な手順 |
| Claude Skills | `.claude/skills/` | Claude Codeが使う同一手順 |

## 標準の拡張機能

### Codex

```powershell
codex.cmd plugin add github@openai-curated-remote
codex.cmd plugin add slack@openai-curated-remote
codex.cmd plugin add codex-security@openai-curated-remote
codex.cmd mcp add exa --url https://mcp.exa.ai/mcp
codex.cmd mcp add context7 --url https://mcp.context7.com/mcp
codex.cmd mcp add playwright -- npx.cmd -y @playwright/mcp@0.0.80
```

ExaまたはContext7が `Not logged in` の場合は、次を実行し、表示された公式認証画面を利用者が操作します。

```powershell
codex.cmd mcp login exa
codex.cmd mcp login context7
```

### Claude Code

```powershell
claude.cmd plugin install github@claude-plugins-official --scope user
claude.cmd plugin install slack@claude-plugins-official --scope user
claude.cmd plugin install claude-security@claude-plugins-official --scope user
claude.cmd plugin install exa@claude-plugins-official --scope user
claude.cmd plugin install context7@claude-plugins-official --scope user
claude.cmd plugin install playwright@claude-plugins-official --scope user
```

既に導入済みで無効な場合は `claude.cmd plugin enable <名前>@claude-plugins-official` を使います。

## 採用しない接続

- Cloudflare操作プラグイン: `main` 反映後のCIだけが公開する方針と競合するため
- 本番DBへ接続するMCP: 誤更新・削除と情報漏えいの影響が大きいため

## 検証

```powershell
node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs" "scripts/agent-harness/**/*.test.mjs"
node scripts/agent-harness/check-skill-parity.mjs
codex.cmd execpolicy check --pretty --rules .codex/rules/default.rules -- git push origin main
```

Skillを更新したら、必ず両方へ同じ変更を行います。CIは内容の差を検出します。

## 関連資料

- `permissions.md`: 権限と認証の方針
- `tool-parity.md`: Claude CodeとCodexの対応表
- `threat-model.md`: 守る対象と残るリスク
- `evaluation-cases.md`: 設定を変えたときの確認項目
- `skills.md`: 外部Skillの出所と固定版
- `changelog.md`: ハーネス変更履歴
