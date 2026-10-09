# TASKS

MyTechPulseの**優先順位の地図**。「いま何が大事で、どの順に進めるか」だけを書く。
個別の進捗・議論・完了条件は **GitHub Issue が正本**で、ここには重複して書かない（#110）。

最終更新: 2026-10-07

## いまの状態

- **本番はGo版**（2026-10-07 に切り替え済み、#127）。画面 https://mytechpulse.net（Cloudflare Pages）、API https://api.mytechpulse.net（AWS Lightsail 1台）。構成の理由は [ADR 0003](./docs/adr/0003-host-on-cloudflare-pages-and-lightsail.md)。
- **自動デプロイは稼働中**（#125、2026-10-08 に有効化。最初の1回は #183 で成功）。`main` に取り込んだ変更が、検査成功後に、自動で本番へ入る（文書・試験だけの変更は反映しない）。止めるときは、リポジトリ変数 `GO_DEPLOY_ENABLED` を削除する。手順は [`docs/deploy/go-auto-deploy.md`](./docs/deploy/go-auto-deploy.md)。
- **旧Python版は、リポジトリから削除済み**。サーバーには、切り戻し用の旧運用ファイル・Python版のイメージ・バックアップを、安定確認まで残してある（[`docs/deploy/rollback-to-python.md`](./docs/deploy/rollback-to-python.md)）。

## 直近の予定（この順に）

1. **バックアップの自動削除の初回動作を確認する**（2026-10-10 04:00 の取得後）。10/1分だけが消え、`.keep` 付きが残ること。→ #179
2. **切り戻し用の削除**。安定したら、サーバー上の旧運用ファイル・Python版のイメージ・古いバックアップと、`rollback-to-python.md` を、許可を得て削除する。→ #128
3. **ドキュメントの統一**。設計資料・運用資料はGo版に合わせ済み。完了したIssueの整理は済み（2026-10-08）。残りは、`dev.ps1` の実機確認と、別サーバーへの移行手順（#55）。→ #129
4. **Caddyの証明書の初回自動更新を確認する**。有効期限は 2026-11-12。APIのドメインをCloudflare経由（プロキシ）にした後の、最初の更新になる。

## A. 一般公開・宣伝の前に必須

- 利用規約・プライバシーポリシーのページと、サインアップの同意（個人情報を集めるのに同意の導線が無い。法的リスクが最も明確）。→ #185
- ログイン試行回数の制限（総当たり対策）と、パスワードの長さ・強度の検証。→ #186
- ログの整備と、本番の監視（障害・容量・外部APIの失敗の検知）。→ #187
- バックアップの外部退避（いまは同じサーバーにだけある。Cloudflare R2など）。→ #188
- オリジンへの直接アクセスを塞ぎ、Cloudflare経由に限定する。実ユーザーを迎える前に。→ #74

## B. 後回し可（機能・品質）

- 記事取得の日次一括化とDB保存（外部APIの上限と、ダウン時の表示のため）。→ #66
- サインアップのタグ一覧を、バックエンドから取る検討。→ #47
- 画面: LPデザイン刷新（#79）、ログイン後の画面の見た目（#83）、ロゴからLPへ戻れない問題（#82）。
- README の Roadmap にある機能（登録後のタグ変更、ブックマーク・既読管理、キーワード検索、ダークモード、AI要約・自動カテゴリ分類）。
- テストの拡充（認証まわり以外の画面・API）。→ #111
- 別サーバーへの移行手順と、2GBプラン（月$12）へ上げる判断基準。→ #55

## 参考（Issueにならない、運用上の決まり）

- `main` の保護（Ruleset）の詳細と、保留中のルール（Require branches to be up to date）は [`CONTRIBUTING.md`](./CONTRIBUTING.md) の4章。
- 本番の構築手順は [`docs/deploy/lightsail-provisioning.md`](./docs/deploy/lightsail-provisioning.md)、バックアップと復元は [`docs/deploy/database-backup-and-restore.md`](./docs/deploy/database-backup-and-restore.md)。
