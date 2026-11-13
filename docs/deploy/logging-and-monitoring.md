# ログの見方と、本番の監視

[Issue #187](https://github.com/H4aruki/MyTechPulse/issues/187) のうち、リポジトリの中で決められることをまとめた資料です。外部の監視サービスへの登録、課金、本番サーバーへの接続はしていません。分からなかったことは「未確認」と書いてあります。

関連: 自動デプロイの失敗時の見方は [go-auto-deploy.md](go-auto-deploy.md)。サーバーの作り方は [lightsail-provisioning.md](lightsail-provisioning.md)。バックアップは [database-backup-and-restore.md](database-backup-and-restore.md)。

## 1. ログはどこに出るか

コンテナのログの出し方は、`docker-compose.yml` も `ops/lightsail-vm-setup.sh` も指定していません。`logging:` の項目も、`/etc/docker/daemon.json` を書く処理も、リポジトリにはありません。

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

`journalctl` で見るのは、ホストの ssh、fail2ban、Docker デーモン自身です。コンテナの中の API のログは、ドライバが `json-file` のままなら `journalctl` には出ません。

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

`journalctl -u docker` は、Docker デーモン自身の記録です。API の JSON はここにはありません（ドライバが既定の `json-file` のとき）。

## 2. 保存期間

リポジトリの中には、Docker のログの回転設定はありません。上に書いたとおり、Docker の既定は上限なしです。本番の `/etc/docker/daemon.json` で上書きされているかは未確認です。

案（まだ入れていません）: 長く動かす `db`、`api-go`、`caddy` の compose に、次を書く。

```yaml
logging:
  driver: json-file
  options:
    max-size: "10m"
    max-file: "5"
```

1サービスあたり約 50MB、3つで約 150MB です。手順書のディスクは 40GB なので、ログだけでディスクを埋める量にはなりません。`docker logs` の見方はそのままです。公式は、回転が最初から付く `local` ドライバも勧めています。この案は、いまの見方（`json-file` と `docker logs`）を変えない方です。どちらにするかは、下の「オーナーが決めること」です。

この設定は、コンテナを作り直したあとから効きます。すでに動いているコンテナには後から付きません（Docker の文書）。`db` と `caddy` は、自動デプロイのたびに作り直されるとは限りません。入れるときは、作り直すタイミングも決めます。

ホストの `/etc/docker/daemon.json` に同じ上限を書く方法もあります。全コンテナに揃いますが、リポジトリの外なので、サーバーを手順書から作り直すと残りません。compose に書くと、次のリリースのファイルと一緒に渡ります。

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
# 環境変数: CHECK_DISK_PERCENT, CHECK_MEM_AVAILABLE_MB, CHECK_SWAP_USED_MB
```

見るのは `/` だけです。手順書のディスクは 1台です。別のマウントがあるかは未確認です。スワップが無い機械は、スワップの項目を通ります。本番は 2GB のスワップを作る手順があるので、作り忘れは別の確認（`free -h`）です。

試験は `bash ops/tests/test_check_resources.sh` です。`df` と `free` の出力を差し替えて、実機の容量は見ません。

### cron に入れる例（本番には入れていない）

成功のときは何も出さないので、cron が出力をメールする設定なら、超えたときだけメールの材料になります。サーバーがメールを送れるかは未確認です。送れない環境では、1行は届きません。ファイルに残す例も下にあります。ファイルを見に行かないと、気づきにはなりません。

超えているあいだは、実行のたびに1行出ます。15分は、ディスク（ゆっくり埋まる）とメモリ（短時間で苦しくなる）の間を取った間隔です。メールがうるさいときは、間隔を広げます。

置く場所は、入れ替えのたびに変わるリリース directory には向きません。`~/MyTechPulse` は `git pull` をしない運用なので、そこへは自動では入りません。配布物（ops の tar）には、このファイルを含む commit が `main` に入ったあとから含まれます。安定したパスへコピーするかは、オーナーが決めます。

```bash
# 例。この行は資料だけで、本番の crontab には入れていない。
*/15 * * * * /path/to/check_resources.sh >> /path/to/resource-check.log 2>&1
```

このスクリプトは消しません。古いイメージの削除は、Issue #187 のコメントのとおり、対象と影響を示して許可を得てからにします。

## 7. 外部の生存監視の候補

登録はしていません。Issue は、API を止めたら数分以内にオーナーへ知らせることを完了条件にしています。自分でサーバーに監視を常駐させる案は、1GB の負担になるため、Issue 本文が後回しにしています。

外から見るなら、`https://api.mytechpulse.net/health/ready` が HTTP 200 であることです。この URL は、公開後の確認として `.github/workflows/ci.yml` に書いてあります。`/health/ready` は DB への ping が失敗すると 503 です。原因の接続情報は応答に入れません（`server/internal/health/handler.go`）。`/health/live` はプロセスが生きていることだけで、DB は見ません。DB が止まったときも知りたいなら、`ready` を見ます。

2026-11-13 に公開ページを見た時点の、無料で使える候補は UptimeRobot です。

- 料金ページ: <https://uptimerobot.com/pricing/>
- 無料枠の説明（2026-09-28 付）: <https://help.uptimerobot.com/en/articles/11604710-who-should-use-uptimerobot-s-free-plan>
- その時点の無料枠: 月額 0、クレジットカード不要、監視 50、間隔 5分、HTTP の監視ができる

5分間隔は、止めてから気づくまで最大でおおよそ5分に、通知の遅れが足されます。Issue の「数分以内」に収まるかは、オーナーの判断です。短い間隔は有料枠です。数値は変わるので、登録の前にもう一度公式ページを見ます。

同じ種類の外形監視は他にもあります。この資料で間隔と件数まで確認したのは、上の1つだけです。ほかを選ぶときは、公式の料金ページで、無料か、間隔、クレジットカードの要否を確認します。

## 8. オーナーが決めること

1. 外部の生存監視を使うか。使うなら、サービス、通知先のメール、無料の5分間隔でよいか、見る URL を `/health/ready` にするか。登録と課金はオーナーが行う。
2. Docker のログ回転を入れるか。入れるなら、上の compose の案か、ホストの `daemon.json` か。入れたあとは、対象のコンテナを作り直さないと効かない。
3. `ops/check_resources.sh` を cron に入れるか。入れるなら、安定したパスと、メールが届くか（届かないなら、ログファイルを誰が見るか）。
4. `provider_request` の行を出すために、起動時の Qiita / Zenn クライアントを `Observed` で包むか。いまのログでは、提供元ごとの失敗は定期取得の `qiita_failed` / `zenn_failed` で数える。
5. 本番の `docker info` の Logging Driver と、バックアップの cron が実際にあるか。この資料では未確認。
