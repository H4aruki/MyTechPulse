# TASKS

MyTechPulseの**優先順位の地図**。「いま何が大事で、どの順に進めるか」だけを書く。
個別の進捗・議論・完了条件は **GitHub Issue が正本**で、ここには重複して書かない（#110）。

最終更新: 2026-10-07

## いまの状態

- **本番はGo版**（2026-10-07 に切り替え済み、#127）。画面 https://mytechpulse.net（Cloudflare Pages）、API https://api.mytechpulse.net（AWS Lightsail 1台）。構成の理由は [ADR 0003](./docs/adr/0003-host-on-cloudflare-pages-and-lightsail.md)。
- **自動デプロイは実装済みで、スイッチ待ち**（#125）。リポジトリ変数 `GO_DEPLOY_ENABLED` を `true` にすると、`main` に取り込んだ変更が、検査成功後に自動で本番へ入る。手順は [`docs/deploy/go-auto-deploy.md`](./docs/deploy/go-auto-deploy.md)。
- **旧Python版は、リポジトリから削除済み**。サーバーには、切り戻し用の旧運用ファイル・Python版のイメージ・バックアップを、安定確認まで残してある（[`docs/deploy/rollback-to-python.md`](./docs/deploy/rollback-to-python.md)）。

## 直近の予定（この順に）

1. **バックアップの自動削除の初回動作を確認する**（2026-10-10 04:00 の取得後）。10/1分だけが消え、`.keep` 付きが残ること。→ #179
2. **自動デプロイを有効にする**。切り替え後の監視（1時間後・翌日）は問題なく完了（#127）。変数を作り、小さな変更で最初の1回を確認する。→ #125
3. **切り戻し用の削除**。安定したら、サーバー上の旧運用ファイル・Python版のイメージ・古いバックアップと、`rollback-to-python.md` を、許可を得て削除する。→ #128
4. **ドキュメントの統一**。設計資料・運用資料はGo版に合わせ済み。残りは、完了している可能性が高いIssueの整理と、手順の実機確認。→ #129
5. **Caddyの証明書の初回自動更新を確認する**。有効期限は 2026-11-12。APIのドメインをCloudflare経由（プロキシ）にした後の、最初の更新になる。

## A. 一般公開・宣伝の前に必須

- 利用規約・プライバシーポリシーのページと、サインアップの同意（個人情報を集めるのに同意の導線が無い。法的リスクが最も明確）。（Issue未起票）
- ログイン試行回数の制限（総当たり対策）と、パスワードの長さ・強度の検証。（Issue未起票）
- ログの整備と、本番の監視（障害の検知）。（Issue未起票）
- バックアップの外部退避（いまは同じサーバーにだけある。Cloudflare R2など）。（Issue未起票）
- オリジンへの直接アクセスを塞ぎ、Cloudflare経由に限定する。実ユーザーを迎える前に。→ #74

## B. 後回し可（機能・品質）

- 記事取得の日次一括化とDB保存（外部APIの上限と、ダウン時の表示のため）。→ #66
- 外部APIの部分的な失敗で、記事の多様性が損なわれる問題への対策。→ #33（Go版は、片方が失敗したら `warnings` で知らせる）
- Qiita記事が複数タグに当たったときの、タグの統合。→ #22
- サインアップのタグ一覧を、バックエンドから取る検討。→ #47
- 開発用のCORS許可オリジンの棚卸し。→ #45
- 画面: LPデザイン刷新（#79）、ログイン後の画面の見た目（#83）、ロゴからLPへ戻れない問題（#82）、アンケート機能（#84）。
- README の Roadmap にある機能（登録後のタグ変更、ブックマーク・既読管理、キーワード検索、ダークモード、AI要約・自動カテゴリ分類）。
- テストの拡充（認証まわり以外の画面・API）。→ #111

## 完了している可能性が高いIssue（オーナーの確認後に閉じる）

Go版への移行で実現済みと思われるもの。コードで確認でき次第、閉じる。

| Issue | 内容 | 実現した方法 |
| --- | --- | --- |
| #14 | APIをHTTPステータス方式へ移行 | Go版は、HTTPステータス＋Problem Details |
| #46 | JWTの保管を localStorage から httpOnly Cookie へ | サーバー側セッション＋HttpOnly Cookie（ADR 0002） |
| #60 | バックエンドのGo移行を検討 | Go版へ移行済み（ADR 0001） |
| #119 | DB変更管理と、移行用の復元検証 | goose による移行と、追加だけの検査 |
| #121 | 認証と利用者管理をGoへ移行 | `server/internal/auth/` |
| #122 | 記事取得と外部メディア連携をGoへ移行 | `server/internal/provider/`、`recommendation/` |
| #126 | データ移行と切り戻しの事前検証 | 合成データで合格（2026-10-06） |
| #110 | 残タスク一覧と課題一覧の二重管理の解消 | この文書 |

## 参考（Issueにならない、運用上の決まり）

- `main` の保護（Ruleset）の詳細と、保留中のルール（Require branches to be up to date）は [`CONTRIBUTING.md`](./CONTRIBUTING.md) の4章。
- 本番の構築手順は [`docs/deploy/lightsail-provisioning.md`](./docs/deploy/lightsail-provisioning.md)、バックアップと復元は [`docs/deploy/database-backup-and-restore.md`](./docs/deploy/database-backup-and-restore.md)。
