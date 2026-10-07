# 双方向workerの運用

## 通常モードと有効化

通常の依頼では、現在のエージェントだけで作業します。親エージェントがworkerを起動できるのは、利用者が `/multi`、`/parallel`、または同等に明確な並列委譲を依頼した場合だけです。

起動前に、次のコマンドでGit、Codex、Claude CodeのCLI状態を確認します。この確認はworker、worktree、branchを作成しません。

```powershell
node scripts/agent-harness/spawn-agent.mjs --check
```

対象workerのCLIが利用できない場合は、worktreeを作成する前に停止して利用者へ報告します。CLIの導入や更新は、この手順では行いません。

## 親ごとの起動例

Codexを親、Claude Codeをworkerにする場合:

```powershell
node scripts/agent-harness/spawn-agent.mjs --activate multi --parent codex --agent claude --worktree login-ui --task "frontend/src/配下のログイン画面だけを担当し、テストとコミットを行う"
```

Claude Codeを親、Codexをworkerにする場合:

```powershell
node scripts/agent-harness/spawn-agent.mjs --activate multi --parent claude --agent codex --worktree auth-api --task "server/internal/auth/配下の認証APIだけを担当し、テストとコミットを行う"
```

`--activate multi` は明示的な有効化です。省略した起動は拒否されます。

## workerの制約

- Codex workerは `gpt-6-luna`、推論量 `high` 以下（ランナーでは `high`）に固定する。
- Claude Code workerは `sonnet`、推論量 `high` 以下（ランナーでは `high`）に固定する。
- 親エージェントのモデルと推論量は変更しない。
- workerは別のworkerを起動しない。
- workerは `agent/<worktree名>` の専用branchと専用worktree内で、割り当てられた範囲だけを変更・検証し、通常コミットを1つ作成する。
- `.env`、認証情報、既存の安全規則、Skills同期方針は通常作業と同じく守る。

## 同じツールを内部サブエージェントとして呼ぶ場合

`/multi` のランナーを使わず、Claude CodeがClaude Codeを、CodexがCodexを内部のサブエージェントとして呼ぶ場合の既定です。

| 親と子 | モデル | 考える深さ | 設定場所 |
|---|---|---|---|
| Claude Code → Claude Code | `sonnet` | `high` | `.claude/settings.json` の `env`（モデル）、`.claude/agents/standard-subagent.md`（モデルと深さ） |
| Codex → Codex | `gpt-6-luna` | `high` | `.codex/config.toml` の `[agents]` |

- Claude Codeは版なしの別名（`sonnet`）で指定する。モデルの更新に自動で追従する。
- Codexの設定にはモデルの版を含む名前しか書けない（版なしの別名は無い）。モデルが更新されたら `.codex/config.toml` の値を手で直す。
- どちらも「指定した段階で固定して呼ぶ」仕組みで、状況に応じて自動で下げる仕組みはない。それより下の段階を使いたいときは、呼ぶ側が個別に指定する。
- Claude Codeの考える深さは、`standard-subagent` を呼んだときだけ固定される。深さだけを指定する環境変数は無い。

## 結果と親の確認

workerは共通JSON Schemaに従い、`status`、`summary`、`filesChanged`、`tests`、`issues`、`commit` を親へ返します。親は担当範囲が重複しないよう事前に分け、結果JSONを確認します。

## Codex workerのログ

Codex workerの作業中の出力は多いため、親へ送らず、worktreeの隣にある `<worktree名>.codex.log` へ書き出します。出力を受け取る領域には約1MBの上限があり、超えるとworkerが途中で強制終了されるためです。

- 親は通常、ログを読まない。結果JSONだけを確認する。
- 失敗した場合だけ、結果の `summary` に載るログ末尾の数行を読む。足りないときも、ログ全文ではなく末尾や該当箇所だけを読む。
- ログは成功・失敗にかかわらず削除しない。

結果を受け取った後は、親が次を確認してから統合の要否を人へ提案します。

1. `git worktree list` でworkerの専用worktreeを確認する。
2. worker branchの差分とコミットをレビューする。
3. 実行済みテスト、未解決課題、他workerとの重複・競合を確認する。

worker結果を自動統合しません。自動merge、rebase、worktree削除、branch削除も行いません。これらは履歴や作業内容を失わせる可能性があり、差分レビューと人の統合判断を先に必要とするためです。
