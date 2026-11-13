# ログの見方と、本番の監視

[Issue #187](https://github.com/H4aruki/MyTechPulse/issues/187) のうち、リポジトリの中で決められることをまとめた資料です。外部の監視サービスへの登録、課金、本番サーバーへの接続、本番への設定の適用はしていません。分からなかったことは「未確認」と書いてあります。

関連: 自動デプロイの失敗時の見方は [go-auto-deploy.md](go-auto-deploy.md)。サーバーの作り方は [lightsail-provisioning.md](lightsail-provisioning.md)。バックアップは [database-backup-and-restore.md](database-backup-and-restore.md)。

## 1. ログはどこに出るか

2026-11-13 の決定で、本番ホストのログドライバは **journald**、保持は**最低 30 日**です（[ADR 0015](../adr/0015-keep-container-logs-30-days-in-journald.md)）。ただし**まだ本番には適用していません**。`docker-compose.yml` には `logging:` を書きません（journald は systemd のある Linux ホスト専用で、開発機が動かなくなるため）。以下の「いまの本番」は、適用前の状態の説明です。

Docker の文書では、指定が無いときのドライバは `json-file` です。コンテナの標準出力と標準エラーを、ホスト上の JSON ファイルに貯め、`docker logs` と `docker compose logs` で見ます。このドライバは、既定ではログを回転しません（`max-size` の既定は `-1`、上限なし）。出典: [Configure logging drivers](https://docs.docker.com/engine/logging/configure/)、[JSON File logging driver](https://docs.docker.com/engine/logging/drivers/json-file/)。

本番ホストの `/etc/docker/daemon.json` と、いま動いているコンテナの設定は、このリポジトリに含まれていません。デーモンが既定を変えているかは未確認です。サーバーに入った人が、次で確認できます。

```bash
docker info --format '{{.LoggingDriver}}'
cid="$(docker ps -q --filter label=com.docker.compose.project=mytechpulse --filter label=com.docker.compose.service=api-go | head -1)"
docker inspect -f '{{json .HostConfig.LogConfig}}' "$cid"
```

`mytechpulse` は `ops/deploy_go.sh` の compose の project 名の既定です。動いている compose ファイルの directory は、同じコンテナのラベル `com.docker.compose.project.working_dir` にあります。リリースの directory は入れ替えのたびに変わるので、ログを見るときは、パスを覚えず、このラベルか `docker logs` を使います。

```bash
dir="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project.working_dir"}}' "$cid")"
docker compose --env-file "$HOME/mytechpulse-production.env" -p mytechpulse \
  -f "$dir/docker-compose.yml" --profile go-preview logs --tail 200 api-go
```

`api-go` は profile `go-preview`、`caddy` は profile `prod` です。`db` に profile はありません。profile を付けないと、compose がそのサービスを知らないことがあります。

いま（適用前）、`journalctl` で見るのは、ホストの ssh、fail2ban、Docker デーモン自身です。コンテナの中の API のログは、ドライバが `json-file` のままなら `journalctl` には出ません。

journald ドライバに切り替えたあとの見方は、Docker の文書（[journald logging driver](https://docs.docker.com/engine/logging/drivers/journald/)、2026-11-13 に確認）に次のとおり書いてあります。
- ログは `journalctl`、journal API、**`docker logs` コマンド**のどれでも取り出せます。つまり、この資料の `docker logs` と `docker compose logs` は、切り替え後も使えます。
- `journalctl` では、コンテナで絞れます。例: `sudo journalctl CONTAINER_NAME=<コンテナ名>`（`-b` で直近の起動以降、`-o json` で JSON）。メタデータとして `CONTAINER_ID`、`CONTAINER_NAME`、`IMAGE_NAME` などが付きます。`CONTAINER_NAME` は、起動時の名前です（`docker rename` は反映されません）。
- 既定ドライバは `daemon.json` に `"log-driver": "journald"` を書き、Docker を再起動して切り替えます。新しく作るコンテナから効き、既存のコンテナは新しい設定を自動では使いません（[Configure logging drivers](https://docs.docker.com/engine/logging/configure/)）。

文書に書いていない次の点は**未確認**です。本番で適用したあとに確かめます。`docker compose logs` が journald でも同じように使えるか（compose は `docker logs` と同じ読み出しを使うはずですが、文書で確認していません）。切り替え後の `docker logs --since 72h` の速さ。`docker logs` の出力と、`journalctl` の出力の細かい違い。

| 対象 | 出る場所 | 見るコマンド |
| --- | --- | --- |
| API | `api-go` コンテナの標準出力。1行が1つの JSON | 上の `docker logs`（サービス名 `api-go`） |
| Caddy | `caddy` コンテナの標準出力・標準エラー | 同じやり方でサービス名 `caddy`。profile は `prod` |
| DB | `db` コンテナの標準出力・標準エラー | 同じやり方でサービス名 `db` |
| 定期取得（articlefetch） | API と同じ標準出力。`component` が `articlefetch` | API と同じ。`ARTICLE_FETCH_ENABLED=true` のときだけ出る |
| バックアップ | スクリプトの標準出力・標準エラー。cron の例では `backups/backup.log` | 下記 |
| ssh / fail2ban / dockerd | ホストの journald | `journalctl -u ssh`、`journalctl -u fail2ban`、`journalctl -u docker` |

### API

`server/cmd/api/main.go` は、`logging.New` にプロセスの標準出力を渡し、レベルは Info 以上です。`server/internal/platform/logging/logging.go` は、そのレベル以上を JSON の1行で書きます。起動のログは `server started` で、待ち受けアドレス（`addr`）と環境の名前（`env`。`local` や `production`）だけです。設定ファイルの中身は出しません。

アクセスの記録は `server/internal/platform/httpx/middleware.go` の `AccessLog` です。出すのは method、path、status、duration、request_id だけです。ヘッダー、クエリ、本文は出しません。

### Caddy

`Caddyfile` と `ops/caddy/Caddyfile.maintenance` に、アクセスログを出す `log` の指定はありません。アクセスの1行ずつを出す設定は、リポジトリにはありません。プロセス自身のメッセージ（起動、証明書、エラー）は、コンテナの標準出力・標準エラーに出ます。このリポジトリは形式を指定していないので、文面の形は未確認です。

### DB

`docker-compose.yml` の `db` は、command も logging も上書きしていません。PostgreSQL の設定ファイルもリポジトリにありません。どの SQL をログに出すかは、イメージの既定です。中身はこの作業では開いていないので、未確認です。SQL 全文やパスワードを出す設定は、リポジトリには書いてありません。

### 定期取得（articlefetch）

`ARTICLE_FETCH_ENABLED=true` のときだけ、API と同じプロセスの中で動きます（`server/cmd/api/main.go` の `startArticleFetch`）。ロガーには `component=articlefetch` が付きます。無効（compose の既定は `false`）のときは、このログは出ません。

本番の環境ファイルで有効になっているかは、この作業ではファイルを読んでいないので未確認です。設計上、本番は有効にする想定です（`AGENTS.md` の記事取得の説明）。

定時は日本時間の 6:00、11:30、16:30 です（`server/internal/articlefetch/schedule.go`）。

1回の終わりに、`article fetch finished` が Info で出ます。属性は `status`、`qiita_requests`、`zenn_requests`、`upserted`、`deleted`、`qiita_pages_ok`、`qiita_failed`、`qiita_invalid_articles`、`zenn_topics_ok`、`zenn_failed`、`zenn_topics_empty` です。`qiita_failed` と `zenn_failed` は、再試行しても取れなかった単位の数です（Qiita は1ページ、Zenn は1トピック）。`qiita_requests` は呼び出した回数で、失敗の回数ではありません。

ほかに、開始（`article fetch started`）、失敗（`article fetch: tag list failed` など、Error）、Qiita の欠けた記事の件数（Warn）が出ます。失敗の説明文は、`fetch_runs.error_message` にも残します。入れてよいのは固定の文と、分類済みの提供元エラーの文だけです（`server/db/migrations/00008_article_batch_fetch.sql` のコメント）。

### バックアップ

`ops/backup_db.sh` は、成功するとダンプのパスを標準出力の最後の1行に出します。失敗と、古い世代の削除の知らせは標準エラーです。冒頭の cron の例は、両方を `backups/backup.log` に足します。

```bash
0 4 * * * cd /path/to/MyTechPulse && ./ops/backup_db.sh >> backups/backup.log 2>&1
```

この cron が本番の crontab に入っているかは、リポジトリからは未確認です。手順書は「登録が要る」と書き、チェックリストに項目があります（[lightsail-provisioning.md](lightsail-provisioning.md)）。登録の有無そのものは、サーバーの crontab を見ないと分かりません。

自動デプロイも、入れ替えの前に同じスクリプトを呼びます（`ops/deploy_go.sh`）。その出力は、GitHub Actions の「サーバーでGo版を入れ替える」のログに出ます。`backups/backup.log` へは、上の cron のリダイレクトがあるときだけ足されます。

### ホスト

`ops/lightsail-vm-setup.sh` は、Ubuntu 24.04 が ssh の記録を journald に置く前提で、fail2ban の ssh の jail を `backend = systemd` にしています。ssh のユニット名は、同じスクリプトが再読み込みする `ssh` です。

```bash
journalctl -u ssh --since "1 hour ago"
journalctl -u fail2ban --since "1 hour ago"
journalctl -u docker --since "1 hour ago"
```

`journalctl -u docker` は、Docker デーモン自身の記録です。API の JSON はここにはありません（ドライバが `json-file` のとき）。journald ドライバでは、コンテナのログは `CONTAINER_NAME=` などで絞って見ます（上）。

## 2. 保存期間

**決定（2026-11-13、K.Haruki）: Docker のコンテナログは最低 30 日残す。** 理由、退けた案（7日・14日、json-file、logrotate、Better Stack Logs）、根拠の数字は [ADR 0015](../adr/0015-keep-container-logs-30-days-in-journald.md) にあります。

いまのリポジトリには、Docker のログの回転設定がありません。Docker の既定は `json-file` で、`max-size` の既定は `-1`（上限なし）です。本番の `/etc/docker/daemon.json` で上書きされているかは未確認です。

決めた方法（**まだ本番には適用していません**）:

- Docker のログドライバは journald。本番ホストの `/etc/docker/daemon.json` に `"log-driver": "journald"`。見本は [`ops/docker-daemon.journald.json`](../../ops/docker-daemon.journald.json)。
- ホストの journald に、次の3つを設定する。drop-in は [`ops/journald-retention.conf`](../../ops/journald-retention.conf)、置き場所は `/etc/systemd/journald.conf.d/` です。

| 設定 | 値 | 意味 |
| --- | --- | --- |
| `MaxRetentionSec` | `30day` | 30日を過ぎたログを消す。保持の本体 |
| `MaxFileSec` | `1day` | journald はファイル単位で消すので、1ファイルを1日分にして、消える単位を1日にする |
| `SystemMaxUse` | `1G` | 安全弁。通常の30日分は数十 MB 程度の見積もり（実測ではない）なので、普段は日数が先に効く。エラーが回り続けたときだけ、日数より前にサイズで消える |

`docker-compose.yml` には書きません。journald は systemd のある Linux ホスト専用で、ローカル開発の Docker Desktop などが動かなくなるためです。入れる手順は [lightsail-provisioning.md](lightsail-provisioning.md) の 6-2、スクリプトは `ops/lightsail-vm-setup.sh`（`LOG_RETENTION=1` のときだけ。まだ本番では走らせていません）です。

適用に必要な作業:

1. drop-in を置き、ホストの journald を再読み込み（`systemctl restart systemd-journald`）。
2. `daemon.json` を置き、Docker を再起動（全コンテナが止まる時間が出る）。
3. `db`、`api-go`、`caddy` を作り直す。Docker の文書では、既存のコンテナは新しいログ設定を自動では使いません。`db` と `caddy` は、自動デプロイのたびに作り直されるとは限りません。作り直すまでは、古い `json-file` のままです。

journald がログをディスクに残す設定か（`/var/log/journal` があるか。無いと再起動で消える可能性）は、Ubuntu 24.04 の既定を確認していないので**未確認**です。適用後に `ls -d /var/log/journal` と `journalctl --disk-usage` で確かめます。

## 3. パスワード、トークン、設定値がログに出ないことの確かめ方

根拠はコードと、その試験です。本番のログをこの作業では取得していません。

- アクセスログは method、path、status、duration、request_id だけです。試験 `TestAccessLogDoesNotLogHeadersOrBody`（`server/internal/platform/httpx/middleware_test.go`）は、本文のパスワードと Cookie の値がログに無いことを見ます。
- panic の値は、応答にもログにも出しません。試験 `TestRecoverDoesNotLogPanicValue` が、それを見ます。
- 提供元1回分のログ属性は、`provider`、`outcome`、`status_class`、`duration_ms` だけです（`server/internal/provider/metrics.go` の `LogAttrs`）。試験 `TestObservedLogsOnlySafeAttributes` は、トークン、検索語、外部の応答本文、認証ヘッダーの名前がログに無いことを見ます。
- 定期取得が `fetch_runs.error_message` に入れる文は、分類済みの説明か固定の文です。`safeText`（`server/internal/articlefetch/job.go`）は、URL の形、認証ヘッダーの名前、`token=` が混ざった文を、固定の「取得に失敗しました」に置き換えます。印の一覧はコードにあります。
- 設定の読み取りエラーは、項目名と、何が違ったかです。値そのものはメッセージに組み立てていません（`server/internal/platform/config/config.go`）。
- パスキーの成否は、数値の利用者 ID、数値のパスキー ID、理由の語を出します（`server/internal/passkey/service.go`）。パスワード、セッショントークン、認証器の中身は出していません。
- バックアップは、パスワードをコマンドの引数にせず、コンテナの環境変数として渡します（`ops/backup_db.sh` のコメント）。スクリプトが標準出力に出すのはダンプのパスです。

サーバーで直近の API ログをざっと見る例です。語が出ても、すぐ秘密とは限りません。行を読んで判断します。

```bash
docker logs --tail 2000 "$cid" | grep -E 'password|DATABASE_URL|QIITA_ACCESS_TOKEN|POSTGRES_PASSWORD' || echo "該当する語はありません"
```

## 4. 提供元（Qiita / Zenn）ごとの失敗の回数

数え方は2つあります。いま本番のログに出るのは、2つ目だけです。

### 4.1 `provider` と `outcome` の行（コードはある。起動経路には無い）

`Observed`（`server/internal/provider/metrics.go`）は、取得1回ごとに `msg` が `provider_request` の1行を出します。失敗は Warn、成功は Info です。`outcome` は `success`、`timeout`、`rate_limited`、`invalid_response`、`unavailable`、`invalid_request` のいずれかです。

`server/cmd/api/main.go` は、Qiita と Zenn のクライアントを `qiita.New` と `zenn.New` で作り、`Observed` で包んでいません。定期取得も同じです。そのため、いまのプロセスはこの行を出しません。下の集計をいまの本番ログに掛けても、件数は 0 です。

行が出るようになったあとの集計です。`jq` は初期設定スクリプトが入れていないので、本番にあるかは未確認です。無いときは、ログをファイルにして、`jq` のある機械で読みます。

```bash
cid="$(docker ps -q --filter label=com.docker.compose.project=mytechpulse --filter label=com.docker.compose.service=api-go | head -1)"
docker logs --since 24h "$cid" 2>&1 \
  | jq -R 'fromjson? | select(.msg == "provider_request" and .outcome != "success")' \
  | jq -s 'group_by(.provider) | map({provider: .[0].provider, failed: length})'
```

`fromjson?` は、JSON でない行（起動失敗の平文など）を飛ばします。

### 4.2 いま出ている定期取得の行

`article fetch finished` の `qiita_failed` と `zenn_failed` が、その1回の失敗の単位数です。直近72時間の合計の例です。

```bash
docker logs --since 72h "$cid" 2>&1 | jq -sR '
  [ split("\n")[] | fromjson? | select(.msg == "article fetch finished") ]
  | {runs: length,
     qiita_failed: (map(.qiita_failed) | add),
     zenn_failed: (map(.zenn_failed) | add)}
'
```

DB の `fetch_runs` には、その回の `status`（`succeeded`、`partial`、`failed`）と `error_message` が残ります。提供元ごとの失敗回数の列はありません。`qiita_requests` と `zenn_requests` は、呼び出した回数です。

即時検索（定期取得が無効のとき）の片方失敗は、フィードの応答の `warnings` に出ます。ログの `provider_request` には、上のとおり出ていません。

## 5. 自動デプロイの失敗に気づく

ワークフロー（`.github/workflows/ci.yml`、`release.yml`）に、Slack やメールを送るステップはありません。通知の設定はこの作業では変えていません。

GitHub の文書では、Actions のメールや画面内の通知は、利用者が通知設定で有効にしたときだけ届きます。有効にすると、自分が起動した workflow の完了（成功、失敗、中立、取り消し）が届きます。失敗だけに絞る選択もあります。設定手順は、通知が「Don't notify」の状態から変える書き方です。

- [Notifications for workflow runs](https://docs.github.com/en/actions/concepts/workflows-and-actions/notifications-for-workflow-runs)
- [Managing GitHub Actions notifications](https://docs.github.com/en/subscriptions-and-notifications/how-tos/managing-github-actions-notifications)

各アカウントのいまの設定は、リポジトリからは未確認です。有効にしていなければ、失敗のメールは来ません。

設定に依存しない見方は、リポジトリの Actions タブです。`main` への push で、検査のあとに「Go版の本番への反映」（`go-deploy`）が動きます。失敗するとその実行が赤くなります。中身は、その実行の「サーバーでGo版を入れ替える」のログで、`deploy:` で始まる行です。戻ったか、戻すことにも失敗したかは [go-auto-deploy.md](go-auto-deploy.md) の4章です。

`GO_DEPLOY_ENABLED` が `true` でないと、この job 自体が動きません。動いていない失敗は、Actions に現れません。

## 6. ディスクとメモリのしきい値

`ops/check_resources.sh` が、ルート（`/`）の使用率、空きメモリ、スワップ使用量を見ます。超えていなければ何も出さず終了コード 0 です。超えたときは終了コード 1 と、警告を1行（複数なら句点でつないだ1行）出します。測れないときと、引数が不正なときは終了コード 2 です。

| 項目 | 既定 | 警告する条件 | 理由 |
| --- | --- | --- | --- |
| ディスク | 80% | 使用率が 80% 以上 | 手順書のディスクは 40GB。80% でも約 8GB 残る。Issue のコメント（2026-10-07）では使用は約 20% で急がなくてよかった。イメージと配布物は消さずに残るので、埋まる前に気づく線として 80% にした |
| 空きメモリ | 100MB | `available` が 100MB 未満 | プランは 1GB。手順書の見込みは合計約 450MB。100MB を切ると、新しいプロセスの余裕が薄い。見るのは `free` の `available`（キャッシュを返せる分を含む）。`free` 列はキャッシュを空きに数えないので、1GB の機械では早く警告になる |
| スワップ | 200MB | 使用量が 200MB 以上 | 手順書は、常時数百 MB のスワップを黄信号としている。200MB はその入口。swappiness は 10 なので、普段はほぼ 0 の想定。合計が 0（スワップが無い）のときは、この項目は見ない |

ちょうど 80% と、スワップちょうど 200MB は警告します。空きメモリがちょうど 100MB のときは警告しません。

引数が環境変数より優先されます。

```bash
ops/check_resources.sh
ops/check_resources.sh --disk-percent 80 --mem-available-mb 100 --swap-used-mb 200
ops/check_resources.sh --record-file /path/to/resource-record.log
# 環境変数: CHECK_DISK_PERCENT, CHECK_MEM_AVAILABLE_MB, CHECK_SWAP_USED_MB, CHECK_RECORD_FILE
```

### 測定値の記録

2026-11-13 の決定で、`check_resources.sh` を cron で回し、毎回の測定値をファイルに残します。サーバー増強の判断や、ログ量・ディスクの増え方を後から見るためです。

`CHECK_RECORD_FILE`（または `--record-file`）でパスを渡したときだけ、測れた回に1行を追記します。しきい値の内側でも外側でも記録します。パスを渡さなければ、いままでどおり記録しません。標準出力の「平常時は何も出さない」は変わりません。

```text
2026-11-13T21:40:00+09:00 disk_used_pct=21 mem_available_mb=412 swap_used_mb=3 swap_total_mb=2048
```

- 日時は日本時間（`+09:00`）。スワップが無い機械（合計 0）は `swap_used_mb=none`。
- 測れなかった回（`df` や `free` の失敗）は、記録しません（終了コード 2）。
- 追記に失敗したとき（親ディレクトリが無い、書けない）は、標準エラーに `警告: 測定値を記録ファイルへ追記できませんでした: <パス>` を1行出します。終了コードは測定の結果のままです（警告があれば 1、無ければ 0）。
- 記録ファイルは自動では回転しません。1行は約 110 バイト、15分間隔で1日 96 行（約 10KB）です。1年でも約 4MB の見積もりです（実測ではない）。

見るのは `/` だけです。手順書のディスクは 1台です。別のマウントがあるかは未確認です。スワップが無い機械は、スワップの項目を通ります。本番は 2GB のスワップを作る手順があるので、作り忘れは別の確認（`free -h`）です。

試験は `bash ops/tests/test_check_resources.sh` です。`df` と `free` の出力を差し替えて、実機の容量は見ません。

### cron に入れる例（本番には入れていない）

成功のときは何も出さないので、cron が出力をメールする設定なら、超えたときだけメールの材料になります。サーバーがメールを送れるかは未確認です。送れない環境では、1行は届きません。測定値の記録と、警告の1行を残す例も下にあります。ファイルを見に行かないと、気づきにはなりません。

超えているあいだは、実行のたびに1行出ます。15分は、ディスク（ゆっくり埋まる）とメモリ（短時間で苦しくなる）の間を取った間隔です。メールがうるさいときは、間隔を広げます。

置く場所は、入れ替えのたびに変わるリリース directory には向きません。`~/MyTechPulse` は `git pull` をしない運用なので、そこへは自動では入りません。配布物（ops の tar）には、このファイルを含む commit が `main` に入ったあとから含まれます。安定したパスへコピーするかは、オーナーが決めます。

```bash
# 例。この行は資料だけで、本番の crontab には入れていない（登録はオーナーが行う）。
# 測定値は resource-record.log に毎回1行追記。警告の1行（と記録の失敗）は resource-check.log へ。
*/15 * * * * CHECK_RECORD_FILE=/path/to/resource-record.log /path/to/check_resources.sh >> /path/to/resource-check.log 2>&1
```

このスクリプトは消しません。古いイメージの削除は、Issue #187 のコメントのとおり、対象と影響を示して許可を得てからにします。

## 7. 外部の生存監視（Better Stack の無料枠）

**決定（2026-11-13、K.Haruki）: 外部の生存監視は Better Stack の無料枠を使う。UptimeRobot は使わない。** 登録・課金はこの PR ではしていません。**登録はオーナーが行います。**

Issue は、API を止めたら数分以内にオーナーへ知らせることを完了条件にしています。自分でサーバーに監視を常駐させる案は、1GB の負担になるため、Issue 本文が後回しにしています。

見る URL の案は `https://api.mytechpulse.net/health/ready` が HTTP 200 であることです。この URL は、公開後の確認として `.github/workflows/ci.yml` に書いてあります。`/health/ready` は DB への ping が失敗すると 503 です。原因の接続情報は応答に入れません（`server/internal/health/handler.go`）。`/health/live` はプロセスが生きていることだけで、DB は見ません。DB が止まったときも知りたいなら、`ready` を見ます。

2026-11-13 に、Better Stack の公開の料金ページで確認した無料枠の内容:

- 個人プロジェクト向け
- 監視（モニター）とハートビートが合わせて 10
- ステータスページ 1
- Slack とメールの通知
- クレジットカード不要
- **チェック間隔は、その時点の公開ページに書いていなかったので未確認**。Issue の「数分以内」に収まるかは、間隔しだいです。**登録の前に、公式ページで再確認します。** 数値は変わるので、登録の前に、ほかの項目ももう一度見ます。
- ログは 3GB、保持 3日。これは外形監視とは別の「ログ製品」の無料枠です。**コンテナログの 30 日保持には使いません**（3日では 30 日の要件を満たさないため。[ADR 0015](../adr/0015-keep-container-logs-30-days-in-journald.md)）。

## 8. 決まったことと、残っていること

決まったこと（2026-11-13、K.Haruki）:

1. Docker のコンテナログは、journald ドライバで最低 30 日残す（2 章、[ADR 0015](../adr/0015-keep-container-logs-30-days-in-journald.md)）。
2. 外部の生存監視は、Better Stack の無料枠（7 章）。
3. `ops/check_resources.sh` を cron で回し、毎回の測定値をファイルに記録する（6 章）。

本番での作業として残っていること（この PR では、どれもしていません）:

- journald の drop-in と `daemon.json` の適用、journald の再読み込み、Docker の再起動、対象コンテナの作り直し。
- `check_resources.sh` の安定したパスへの配置と、本番の crontab への登録。サーバーがメールを送れるか、記録ファイルを誰が見るかも決める。
- Better Stack の登録（オーナー）と、チェック間隔の再確認。登録するまで、Issue #187 の「止まったら数分で通知」は満たしません。
- 本番の `docker info` の Logging Driver と、バックアップの cron が実際にあるか。この資料では未確認。

まだ決めていないこと:

- `provider_request` の行を出すために、起動時の Qiita / Zenn クライアントを `Observed` で包むか。いまのログでは、提供元ごとの失敗は定期取得の `qiita_failed` / `zenn_failed` で数える。
