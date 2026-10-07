# Python版へ戻す手順（緊急用）

本番のGo版に重大な問題が出て、直前の版へ戻しても直らないときに、**Python版へ戻す**手順です。2026-10-07 にGo版へ切り替えた（#127）際に、サーバーへ残した切り戻しの手段を使います。

- いつまで使えるか: 次のものがサーバーに残っている間です。**#128 でこれらを削除するときに、この文書も削除します。**
- 使う前に、オーナーが判断します。Go版を直して出し直す方が、早くて安全なことが多いです。
- データベースは巻き戻しません。Go版が追加した列と表は、Python版が無視します。Go版で登録した利用者も、Python版で同じパスワードでログインできます。

## サーバーに残してあるもの

| もの | 場所・名前 |
| --- | --- |
| 旧運用ファイル（Python版のcompose・Caddyfile・バックアップ用スクリプト） | `/home/ubuntu/MyTechPulse` |
| Python版のDockerイメージ | `mytechpulse-legacy-api:pre-go-20261007`（動かしていた名前は `mytechpulse-api:latest`） |
| 切り戻しのスクリプトを持つ、切り替えの時のrelease | `/home/ubuntu/releases/a7848f65b66e9783016593842a62c38a96bfe4ea-37572900015-1/` |
| 切り替え直前のバックアップ | `/home/ubuntu/MyTechPulse/backups/mytechpulse_20261007T045240Z.dump` |
| 切り替え前の画面（Cloudflare Pages）の公開 | 識別子 `ccd95985-0a47-412a-acd7-53ccb7159710` |

**注意**: バックアップは、取得のたびに、7日を超えた古い世代が自動で削除されます（`ops/prune_backups.sh`）。切り替え直前のバックアップを、切り戻しのために残すには、同じ場所に `mytechpulse_20261007T045240Z.dump.keep`（空のファイル）を作ります（`touch`）。`.keep` があるバックアップは、削除されません。

## 手順

### 0. 自動デプロイを止める（先に行う）

戻している最中に、自動デプロイが動いて、Go版が入れ替わるのを防ぎます。GitHub のリポジトリで、変数 `GO_DEPLOY_ENABLED` を**削除する**（または `false` にする）。Settings → Secrets and variables → Actions → Variables。

### 1. サーバーでAPIを戻す

サーバーにログインして、次を実行します。

```bash
bash /home/ubuntu/releases/a7848f65b66e9783016593842a62c38a96bfe4ea-37572900015-1/ops/cutover.sh rollback
```

これは、窓口をメンテナンスにする → Go版を止める（消さない）→ Python版APIを起動する → 稼働確認 → 窓口をPython版へ戻す → 本番のホスト名で確認、を順に行います。最後に `cutover: rollback ok` が出れば成功です。一部が失敗した場合は、失敗した工程が表示されるので、その工程を手で続けます。

### 2. 画面を、切り替え前の公開へ戻す

自分のPCで次を実行します。Go版用の画面は、Python版のAPIと組み合わせられません。

```bash
gh workflow run cutover-frontend.yml -R H4aruki/MyTechPulse -f action=rollback -f rollback_to=ccd95985-0a47-412a-acd7-53ccb7159710
gh run watch -R H4aruki/MyTechPulse
```

うまくいかなければ、Cloudflare Pages の管理画面（Deployments）で、上の識別子の公開を選んで「Rollback」します。

### 3. 確認

- `https://mytechpulse.net/` を強制再読み込み（`Ctrl+Shift+R`）して、ログインできる。
- 記事の一覧が出る。

## 戻した後

- 利用者は、もう一度ログインし直しになります（Python版とGo版は、ログインの仕組みが違うため）。
- Go版を直したら、改めて切り替えます。そのときは、自動デプロイではなく、手順を決めて行います（Go版とPython版の画面・APIの組み合わせは、同時に切り替える必要があるため）。
