# Skillsの管理

## 外部Skill

導入時の内容を固定し、Codex用とClaude Code用へ同じものを配置しています。

| Skill | 出所 | 固定コミット |
|---|---|---|
| `fastapi` | `fastapi/fastapi` の `fastapi/.agents/skills/fastapi` | `50113da16fec53b66b80d75e80a89296de4fa5a5` |
| `react-best-practices` | `vercel-labs/agent-skills` の `skills/react-best-practices` | `063bee94c3f4df8453406c830b0a7df0f2860278` |
| `webapp-testing` | `anthropics/skills` の `skills/webapp-testing` | `41bbe19d1a1a7eaab5e7bb9050a417e5c6cffc8f` |

更新時は新しいコミットを指定して両側へ導入し、配布元との差分とライセンスを確認します。

## プロジェクト固有Skill

- `verify-change`: 変更範囲に合う検証
- `prepare-pr`: コミットとPR準備
- `review-change`: 差分レビュー
- `debug-failure`: 証拠を基にした障害調査
- `harness-maintenance`: 共通設定の同期
- `mytechpulse-domain`: 推薦と記事順位の仕様
- `api-contract`: APIとフロント型の同期
- `auth-security-review`: JWT、権限、秘密情報の確認
- `database-change`: 既存データを守るDB変更
- `frontend-implementation`: 画面実装と操作確認
- `release-readiness`: PR提出前の公開準備確認

## 更新後の確認

```powershell
python -X utf8 C:\Users\<user>\.codex\skills\.system\skill-creator\scripts\quick_validate.py .agents\skills\<skill-name>
python -X utf8 C:\Users\<user>\.codex\skills\.system\skill-creator\scripts\quick_validate.py .claude\skills\<skill-name>
node scripts/agent-harness/check-skill-parity.mjs
```
