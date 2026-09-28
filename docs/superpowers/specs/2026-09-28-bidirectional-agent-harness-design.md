# 双方向 Agent Harness 設計

**日付:** 2026-09-28
**関連Issue:** #132

## 目的

Codex と Claude Code のどちらから作業を始めても、利用者が明示的に許可した場合だけ、もう一方を専用 Git worktree の worker として起動できるようにする。通常の開発作業は、起動中のエージェントだけで完結する。

## 現状と再利用する構成

- `AGENTS.md` は Codex と Claude Code が共有する規則の正本であり、`CLAUDE.md` はそれを読み込む薄い入口である。
- `.claude/hooks/` は共通の安全フック実装であり、`.codex/hooks.json` は Codex 側から同じ実装を呼び出す設定である。
- `scripts/agent-harness/check-skill-parity.mjs` と既存の Node.js テストは、設定の同期を検証している。
- 参照した Mikutya-Event-Calendar の `AGENT_HARNESS` は、エージェントごとの差を設定ファイルへ閉じ込め、Node.js の共通コマンドと `--check` を提供している。

既存の安全フック、Skills の同期検証、Codex の実行規則は置き換えない。新しい機能は `scripts/agent-harness/` の下へ追加する。

## 明示的な有効化境界

親エージェントは、利用者の依頼に次のいずれかが含まれる場合だけランナーを呼び出せる。

- `/multi`
- `/parallel`
- Codex と Claude Code を並列・分担・サブエージェントとして使う、という同等の明確な自然言語指示

ランナーは `--activate multi` を必須にする。指定がなければ終了コード 2 で拒否する。これは、通常時に誤ってランナーを実行した場合の機械的な防止策である。

CLI だけでは、渡された依頼文が本当に利用者の発言かを検証できない。そのため、親エージェントが明示的な利用者指示を確認する規則を `AGENTS.md` に置き、ランナーの必須フラグと二重に守る。

## コンポーネント

### `scripts/agent-harness/agent-harness.config.mjs`

`AGENT_HARNESS` を正本として定義する。

- `codex` と `claude` の表示名、実行ファイル名、非対話コマンド組み立て
- worker の最終結果 JSON Schema の場所
- worktree の親ディレクトリ名と、worker ブランチの `agent/` 接頭辞
- Windows では `.cmd` を付け、macOS/Linux ではそのまま実行する変換

CLIの呼び出しはこのファイル以外へ直接書かない。

### `scripts/agent-harness/worker-result.schema.json`

worker が親へ返す最終結果の JSON Schema を定義する。必須フィールドは次のとおり。

```json
{
  "status": "completed | blocked | failed",
  "summary": "担当作業の要約",
  "filesChanged": ["変更ファイル"],
  "tests": ["実行した検証と結果"],
  "issues": ["警告または未解決事項"],
  "commit": "worker が作成したコミットSHA、なければ空文字列"
}
```

Codex は `codex exec --output-schema`、Claude Code は `claude -p --output-format json --json-schema` を使う。ランナーは各CLIの出力をこの共通形式として標準出力へ返す。

### `scripts/agent-harness/spawn-agent.mjs`

共通の非対話起動入口とする。

```text
node scripts/agent-harness/spawn-agent.mjs \
  --activate multi \
  --agent claude \
  --worktree ui-login \
  --task "ログイン画面だけを担当してください。..."
```

起動処理は次の順で行う。

1. 引数を検証し、`--activate multi`、対応エージェント、非空のタスク、worktree名を要求する。
2. `AGENT_HARNESS_ROLE=worker` のときは、再帰委譲として拒否する。
3. Git リポジトリと対象 CLI の存在を確認する。
4. `<リポジトリ親>/<リポジトリ名>-worktrees/<worktree名>` を作業場所として決める。
5. `git worktree add -b agent/<worktree名> <場所> HEAD` で専用ブランチと worktree を作る。既存の場所・既存ブランチには上書きしない。
6. worker の環境へ `AGENT_HARNESS_ROLE=worker`、`AGENT_HARNESS_PARENT=codex|claude` を設定する。
7. 目的、担当範囲、編集禁止範囲、完了条件、テスト、結果Schema、コミット要求を含む worker プロンプトを渡す。
8. worker の構造化結果を検証し、親が読める JSON として標準出力へ返す。

`--check` は Git、Codex、Claude Code の `--version` を確認し、インストール状況だけを JSON で返す。worktree作成やエージェント起動はしない。

ランナーは `git merge`、`git rebase`、`git worktree remove`、`git clean`、強制pushを実行しない。worker worktree の削除も利用者または親エージェントが明示的に管理する。

## worker の権限と再帰防止

- Codex worker は対象 worktree を `-C` で指定し、`workspace-write` を使う。`--full-auto` と `--dangerously-bypass-approvals-and-sandbox` は使わない。
- Claude worker は `--permission-mode dontAsk` と既存の `.claude/settings.json` の許可規則を使う。workerのコミットに必要な `git add` と `git commit` は、危険なGit操作を含まない許可規則として追加する。
- 両方ともプロジェクトの `.env` 拒否、安全フック、ネットワーク方針を継承する。
- worker プロンプトと `AGENTS.md` は、worker が `spawn-agent.mjs`、`claude -p`、`codex exec` による別エージェント起動を行わないことを要求する。
- ランナーが worker 環境で実行された場合も拒否するため、指示だけに依存しない。

## 親の責務と統合

親は明示的有効化後にだけ、変更対象ファイルが重ならない独立した担当を割り当てる。workerごとに worktree と `agent/<worktree名>` ブランチを分離する。

worker完了後、親は次を確認する。

1. 結果 JSON の `status`、`issues`、`tests`。
2. `git -C <worktree> status --short` と workerブランチの差分。
3. workerコミットの内容と、主作業とのファイル衝突。
4. 必要な追加検証。

親は結果を無条件に採用しない。統合は親が差分をレビューしてから行い、マージの最終判断は人間が行う既存規則に従う。

## 文書と設定

- `AGENTS.md` には、通常時は単独作業、明示的有効化、親・workerの責務、再帰禁止の短い規則だけを追加する。
- `CLAUDE.md` は共通規則への参照を維持し、Claude Code固有の非対話worker入口を案内する。
- `.codex/hooks.json` は既存の共通安全フック設定として復元する。
- `docs/agent-harness/README.md` は通常モード、明示的有効化、双方向起動、結果、CLI確認を案内する。
- `docs/agent-harness/multi-agent.md` はworktree、担当分割、統合、トラブルシューティングを詳述する。
- リポジトリへ未追跡で混入した外部Skillsは削除する。既存の追跡済みSkillsは変更せず、外部Skillsの導入状態はユーザー領域で管理する。

## 検証

Node.js標準の `node:test` で次を確認する。

- `--activate multi` がなければ拒否される。
- worker環境からの再帰起動が拒否される。
- `--check` がエージェントを起動せずCLI検査だけを組み立てる。
- Codex と Claude Code のコマンドが共通Schemaと対象worktreeを使う。
- 無効なworktree名、空タスク、未知のエージェントが拒否される。
- workerプロンプトに担当範囲・編集制約・完了条件・テスト・結果形式が含まれる。

既存の `.claude/hooks`、`.codex/hooks`、Skill同期、Codex規則の検証も実行する。実際のworker起動は検証では行わないため、課金、外部通信、worktree作成を伴わない。

## 制約

- ランナーはユーザーの明示的な依頼そのものを認証できない。親エージェントの共通規則と必須の `--activate multi` を組み合わせて防ぐ。
- workerの実行には、対象CLIのログイン済み状態とローカル導入が必要である。未導入・未ログイン時は `--check` と明確なエラーで案内する。
- worktreeの削除とブランチ統合は自動化しない。既存変更を失わないためである。
