# 意思決定ログ（ADR）

このフォルダ `docs/adr/` が、MyTechPulse の意思決定ログの唯一の置き場です。2つ目のフォルダは作りません。

- 1決定1ファイル。ファイル名は `NNNN-英小文字ハイフン.md`。番号は続き番号です。
- 書き方は [0000-template.md](0000-template.md) に従います。先頭に `status: proposed / accepted / superseded` を書きます。一部だけ未確定なら、`proposed` か `accepted` のどちらかにして、未確定の部分を本文に明記します。
- 議論は [決定の記録用のIssueテンプレート](../../.github/ISSUE_TEMPLATE/decision.yml) のIssueで行い、決まったらADRにします。
- 決定を含むPRは、ADRを追加・更新し、PR本文にも要約を書きます。食い違ったらADRを正とします。
- 決めたことを変えるときは、古いADRを書き換えず、新しいADRで置き換えます（古い方の `status` を `superseded` にします）。
- レビューでは、ここにある決定済みの事項を指摘しません。

## 詳しさの要件

意思決定ログは最も重要な成果物の一つです（Harukiさん、2026-11-13 16:43 JST ごろ、親エージェント経由の指示）。できるだけ詳しく書きます。各決定について、次の9項目を**必ず**書きます。短いADRも同じ項目を持ちます。当てはまらない項目は、空欄にせず「なし」と理由を書きます。

| 項目 | 書くこと |
|---|---|
| 背景と問題 | 何が起きていて、なぜ決める必要があるか |
| 検討した案 | 退けた案を含め、それぞれ採らなかった理由 |
| 決定 | 何を、誰が、いつから、どこまで |
| 本人の発言 | 原文の引用、日時（JST）、出典の場所。原文が無ければ「記録なし」 |
| 理由（判断基準） | 何を一番重く見て選んだか |
| 根拠のデータ・事実 | 数値、確認したコマンドやAPI、日時 |
| 結果と代償 | トレードオフ、残るリスクと対策 |
| 見直し方 | いつ、何を見て、誰が見直すか |
| 関連リンク | Issue、PR、設計書、外部資料 |

守ること:

- **引用は資料にある原文だけ**。言い換えを引用符で囲まない。作らない。
- 決定が複数ある作業は、決定ごとにファイルを分ける。全体は概要のADR（索引）に置く。
- 時刻は日本時間（JST）。分からないときは推測せず「時刻の記録なし」と書く。
- 図は mermaid でよい。図の直後に、その図が示すことを1文で書く。

| 番号 | 題名 | 状態 |
|---|---|---|
| [0001](0001-use-go-modular-monolith.md) | Go製モジュラーモノリスを採用する | accepted |
| [0002](0002-use-server-side-sessions.md) | サーバー側認証セッションを採用する | accepted |
| [0003](0003-host-on-cloudflare-pages-and-lightsail.md) | 画面をCloudflare Pages、API・DBをAWS Lightsail 1台に置く | accepted |
| [0004](0004-automerge-small-prs-by-deterministic-script.md) | 小さいPRの自動マージを、決定的なスクリプトで行う | accepted |
| [0005](0005-eligibility-decided-by-deterministic-script.md) | 自動マージの可否は、決定的な判定スクリプトが決める | accepted |
| [0006](0006-merge-with-box-gh-and-automerge-sh.md) | マージは box の gh と automerge.sh で行う | accepted |
| [0007](0007-no-daily-cap-size-limits-weekly-review.md) | 1日の上限は設けず、大きさの上限を毎週見直す | accepted |
| [0008](0008-deploy-latest-main-without-cancelling.md) | デプロイは打ち切らず、最新の main を出す | accepted |
| [0009](0009-automerge-safety-guards.md) | 自動マージの安全装置 | accepted（時間帯の「人がいる時間」は未確定） |
| [0010](0010-pr-template-v2-evidence-first.md) | PRテンプレート v2 は、証拠と図を先に見せる | accepted |
| [0011](0011-automerge-rollout-phases.md) | 自動マージは段階を踏んで始める | accepted（phase 1 以降は未実施） |
| [0012](0012-allow-go-imports-in-go-mod.md) | Goのimportは、mainのgo.modにある依存を許す | accepted |
