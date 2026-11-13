# 意思決定ログ（ADR）

このフォルダ `docs/adr/` が、MyTechPulse の意思決定ログの唯一の置き場です。2つ目のフォルダは作りません。

- 1決定1ファイル。ファイル名は `NNNN-英小文字ハイフン.md`。番号は続き番号です。
- 書き方は [0000-template.md](0000-template.md) に従います。先頭に `status: proposed / accepted / superseded` を書きます。
- 議論は [決定の記録用のIssueテンプレート](../../.github/ISSUE_TEMPLATE/decision.yml) のIssueで行い、決まったらADRにします。
- 決定を含むPRは、ADRを追加・更新し、PR本文にも要約を書きます。食い違ったらADRを正とします。
- 決めたことを変えるときは、古いADRを書き換えず、新しいADRで置き換えます（古い方の `status` を `superseded` にします）。
- レビューでは、ここにある決定済みの事項を指摘しません。

| 番号 | 題名 | 状態 |
|---|---|---|
| [0001](0001-use-go-modular-monolith.md) | Go製モジュラーモノリスを採用する | accepted |
| [0002](0002-use-server-side-sessions.md) | サーバー側認証セッションを採用する | accepted |
| [0003](0003-host-on-cloudflare-pages-and-lightsail.md) | 画面をCloudflare Pages、API・DBをAWS Lightsail 1台に置く | accepted |
| [0004](0004-automerge-small-prs-by-deterministic-script.md) | 小さいPRの自動マージを、決定的なスクリプトで行う | accepted |
