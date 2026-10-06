# Go移行リハーサル手順

Go版へ切り替える前に、本番相当のデータを使って「移行」「主要な操作」「切り戻し」を、本番から隔離した環境で通して確認します。#126 の手順です。合格するまで #127（本番切り替え）は始めません。

- 作業場所: オーナーのPCの中だけ（Windowsの Git Bash を使います）。本番サーバー・本番データベースには接続しません。
- 本番由来のバックアップ（以下「バックアップ」）は、暗号化した1ファイル（`.gpg`）としてだけ扱います。
- ブラウザの操作は人間が行います。

## 全体の流れ

1. 事前の承認と保管場所の確認（§1）
2. 準備: 配布物を作る → 手元へ取り出す → 旧版を用意 → バックアップを置く（§2）
3. 自動のリハーサルを実行する（§3）
4. ブラウザで確認する（§4）
5. 結果を記録して合否を判定する（§5）
6. #127 が終わって安定してから、承認を得て片付ける（§6）

次のどれかに当たったら、そこで中止して状況を記録し、オーナーに相談します（機密値は書きません）。

- 配布情報・実行回・成果物の組み合わせが一致しない
- 本番への接続、意図しないデータ変更、秘密情報の表示が疑われる
- 移行前後の件数・制約・採番の検査に失敗する
- 既存データの比較に不一致がある、または比較できない
- 動作確認・ブラウザ確認・切り戻しのどれかに失敗する
- 停止相当の工程が30分を超える

## 1. 事前の確認と承認

作業を始める前に、オーナーと作業者で次を確認します。1つでも確認できなければ、バックアップを用意せず、リハーサルも始めません。

| 確認項目 | 確認 |
| --- | --- |
| 使う場所はオーナーのPC内の、本番から隔離した環境だけである | □ |
| バックアップは `gpg` のパスワード方式で暗号化して使う | □ |
| 暗号化したバックアップの置き場所は、リポジトリ・自動同期のフォルダ（OneDrive、iCloud など）・Obsidian の Vault のどれの中でもない | □ |
| 暗号化のパスワードはパスワード管理ツールに保管する。リポジトリ・チャット・Issue・ログ・コマンドの引数・環境変数には書かない | □ |
| バックアップの利用と、隔離環境での復元・移行・ブラウザ確認について、オーナーの承認を得た | □ |
| 本番サーバー・本番データベースへ接続しないことを確認した | □ |

承認者：＿＿＿＿＿＿＿＿　確認日：＿＿＿＿年＿＿月＿＿日

## 2. 準備

### 2-1. 配布物を作る（GitHub 上）

1. GitHub の Actions で `Release artifacts` を手動実行します（`main` を選びます）。またはコマンドで `gh workflow run release.yml --ref main`。
   - これで API の箱（Docker イメージ）が GHCR に送られます。外部への送信にあたるので、オーナーの了解を得てから実行します。
2. GHCR のパッケージ `mytechpulse-api-go` は**公開**で運用します（2026-10-06 オーナー決定。初回の配布で公開として作られました）。公開なので、一度送った箱の中身は取り消せません。次の §2-2 の確認を、配布のたびに行います。取り出しにログインやトークンは要りません。
3. 実行結果のページ（Summary）に出る次の値を控えます。Issue へは、値そのものでなく「照合できたか」だけを書きます。
   - 実行回の ID と attempt（番号）
   - コミットの SHA
   - Manifest SHA256

### 2-2. 箱の中身を確認する（配布のたび）

箱は公開のため、秘密の値が入っていないことを、送った箱そのものから確かめます。

```bash
docker pull "ghcr.io/h4aruki/mytechpulse-api-go@sha256:<manifestのdigest>"
docker create --name mtp-inspect "ghcr.io/h4aruki/mytechpulse-api-go@sha256:<manifestのdigest>"
docker export mtp-inspect | tar -t | grep -v '/$'
docker image inspect "ghcr.io/h4aruki/mytechpulse-api-go@sha256:<manifestのdigest>" --format '{{json .Config.Env}}'
docker rm mtp-inspect
```

期待する中身: ファイルは `api`・`migrate`・`etc/ssl/certs/ca-certificates.crt` と、Docker が付ける空の項目（`.dockerenv`、`dev/console`、`etc/hostname`、`etc/hosts`、`etc/mtab`、`etc/resolv.conf`）だけ。環境変数は `PATH` だけ。これ以外が出たら中止して、オーナーに知らせます。公開済みの箱に秘密が入っていた場合は、パッケージのその版の削除と、秘密の無効化（作り直し）が必要です。

あわせて、ビルドの履歴に秘密らしき語が無いことも確かめます。

```bash
docker history --no-trunc "ghcr.io/h4aruki/mytechpulse-api-go@sha256:<manifestのdigest>" --format '{{.CreatedBy}}' | grep -iE "token|secret|password|key" || echo なし
```

### 2-3. 配布物を手元へ取り出す

作業フォルダは、リポジトリ・自動同期フォルダ・Vault の外に作ります（例: `/c/work/mtp-rehearsal`）。パスは英数字と `.` `_` `/` `-` だけにします。

```bash
RUN_ID=<実行回のID>
SHA=<コミットのSHA>
ATTEMPT=<attempt>
mkdir -p /c/work/mtp-rehearsal/archives /c/work/mtp-rehearsal/releases
cd /c/work/mtp-rehearsal/archives
gh run download "$RUN_ID" -R H4aruki/MyTechPulse -n "frontend-$SHA"
gh run download "$RUN_ID" -R H4aruki/MyTechPulse -n "ops-$SHA"
gh run download "$RUN_ID" -R H4aruki/MyTechPulse -n "release-manifest-$SHA-$ATTEMPT"
ls
```

`frontend-<SHA>.tar.gz`、`ops-<SHA>.tar.gz`、`release-manifest.json` の3つが、このフォルダの直下にあることを確認します（サブフォルダに入っていたら、直下へ移します）。この3つは同じ実行回のものだけを使います。APIだけ別の版に替えたり、画面を作り直したりしません。

### 2-4. 旧版（Python版）を用意する

切り戻しの模擬では、直前のリリース（いま本番で動いている Python 版 API）を同じ隔離環境で起動します。

1. 本番で動いているコミットのソースで、Python 版の箱を手元で作ります。コミットが分からなければ最新の `main` を使います。
   ```bash
   docker build -f backend/Dockerfile -t mytechpulse-legacy:rehearsal .
   ```
2. 旧版の運用ファイル一式を置くフォルダを用意します（`docker-compose.yml` が入っていること）。例: そのコミットの `docker-compose.yml` を `/c/work/mtp-rehearsal/old-release/` へコピー。
3. 切り戻し先の記録（`previous-release.json`）を、次の形で作ります。値は英数字と `. _ : / @ + = -` だけです。画面（フロントエンド）の項目は、本番の Cloudflare Pages の直前の公開の識別子を書きます（この環境では起動しないため、書式を満たす値で構いません）。
   ```json
   {
     "manifest_sha256": "<64桁>",
     "api_image": "mytechpulse-legacy:rehearsal",
     "frontend_deployment_id": "<直前の公開のID>",
     "frontend_artifact_name": "frontend-previous",
     "frontend_sha256": "<64桁>",
     "ops_artifact_name": "ops-previous",
     "ops_sha256": "<64桁>",
     "ops_release_dir": "/c/work/mtp-rehearsal/old-release"
   }
   ```

### 2-5. バックアップを置く（承認後・人間）

§1 の承認を得てから行います。Codex などの AI は本番へ接続しません。

1. 本番サーバーで、最新のバックアップ（`backups/` の最新の `.dump`）を `gpg --symmetric --cipher-algo AES256` で暗号化し、暗号化した1ファイルだけをオーナーのPCへ持ち出します。パスワードは入力欄に人が入力します。サーバー側の `backups/` は消しません。
2. PC の作業フォルダ（例: `/c/work/mtp-rehearsal/dumps/`、リポジトリ・自動同期フォルダ・Vault の外）へ置きます。
3. 同じ場所に、チェックサムのファイルを作ります。
   ```bash
   cd /c/work/mtp-rehearsal/dumps
   sha256sum rehearsal.dump.gpg > rehearsal.dump.gpg.sha256
   ```
4. 暗号化せずに持ち出した平文のファイルが PC に残っていないか確認します。残っていたら、消す対象と理由をオーナーに示して承認を得てから消します。

## 3. 自動のリハーサルを実行する

隔離用のパスワード（本番のものを使い回さない、この環境専用の使い捨て）を作ってから、実行します。暗号化パスワードは、実行中に出る入力欄へ人が入力します。

```bash
cd <このリポジトリ>
export MTP_REHEARSAL_DB_PASSWORD="$(head -c 18 /dev/urandom | od -An -tx1 | tr -d ' \n')"
export MTP_RELEASE_MANIFEST=/c/work/mtp-rehearsal/archives/release-manifest.json
export MTP_MANIFEST_SHA256=<Summary に出た Manifest SHA256>
export MTP_RELEASE_RUN_ID=<実行回のID>
export MTP_RELEASE_RUN_ATTEMPT=<attempt>
export MTP_RELEASES_ROOT=/c/work/mtp-rehearsal/releases
export MTP_REHEARSAL_DUMP=/c/work/mtp-rehearsal/dumps/rehearsal.dump.gpg
export MTP_PREVIOUS_RELEASE_RECORD=/c/work/mtp-rehearsal/previous-release.json
bash ops/rehearsal.sh /c/work/mtp-rehearsal/archives
```

- 出力は工程名・秒数・成功/失敗だけです。工程は次の順に進みます: `inputs`（入力の確認）→ `release-verify`（配布物の検証）→ `previous-release`（切り戻し先の確認）→ `image-pull` → `decrypt`（復号）→ `db-start` → `backup-verify` → `restore`（復元）→ `snapshot-before` → `migrate`（移行）→ `snapshot-after` → `compare`（内容比較）→ `serve`（Go版の起動）→ `smoke`（動作確認）→ `expected-diff` → `cleanup-compare`（合成データの片付けと再比較）→ `rollback`（旧版へ戻す模擬）→ `switch-back`（Go版へ再切替）。
- どれかが失敗したら、そこで止まります。失敗した工程の名前を記録して、オーナーに相談します。
- 暗号化パスワードは、`decrypt` の工程で `gpg` が入力を求めます。
- 最後に `rehearsal: stop-equivalent …s, rollback …s, total …s`、`rehearsal: database <DB名>`、`rehearsal: ok` が出ます。**DB名は §4 で使うので控えます**（合成の名前で、秘密ではありません）。
- `stop-equivalent`（移行前の記録から動作確認の終わりまで）が 1800 秒（30分）を超えると不合格です。
- 終了時、コンテナは止まるだけです。復元したDBとボリュームは残ります（消えるのは、この実行で作った一時フォルダ＝復号したファイル・記録だけ）。再実行するときは、新しいDBが同じボリュームに追加されます。
- 復号した一時ファイルが残っていないことは、スクリプトが自動で片付けます。念のため、`$TMPDIR`（なければ `/tmp`）に `tmp.*` フォルダが残っていないことを目で確認します。

## 4. ブラウザで確認する（人間）

画面は本番のAPIのホスト名（`api.mytechpulse.net`）を埋め込んで作られています。隔離環境をその名前で起動し、ブラウザだけをそこへ向けます。PC の hosts ファイルや 443 番ポートは変更しません。

```bash
export MTP_REHEARSAL_RELEASE_DIR=/c/work/mtp-rehearsal/releases/<SHA>-<RUN_ID>-<ATTEMPT>
export MTP_REHEARSAL_DB_NAME=<§3 の最後に出た DB名>
# MTP_REHEARSAL_DB_PASSWORD は §3 と同じ値でなくても構いません（起動時に設定し直します）。未設定なら新しく作ってください
bash ops/rehearsal_browser.sh up production
```

`up production` は本番と同じ設定（Cookie に `__Host-` と Secure が付く、Swagger UI は 404）で起動します。`rehearsal-browser: swagger 404` が出れば、本番相当の設定で Swagger UI が出ないことの確認になります。

専用のブラウザ設定（profile）で開きます。本物の本番サイトを見たことのある通常のブラウザは使いません（本番サイトの記録が残っていて、隔離環境の証明書を受け付けない場合があるため）。

```text
chrome --user-data-dir=<新しい空のフォルダ> --host-resolver-rules="MAP api.mytechpulse.net 127.0.0.1:18443" https://api.mytechpulse.net/app/
```

隔離環境の証明書は使い捨てなので警告が出ます。この専用 profile でだけ続行します。確認する項目は次のとおりです。

1. 既存の利用者（本番のバックアップ由来）でログインし、記事が表示される。**既存の利用者の興味度を変える操作（記事のクリックなど）はしません。**
2. 合成の利用者（`rehearsal-smoke-` で始まる名前は自動の動作確認用に使われているので、ブラウザ用には別の名前、例 `rehearsal-browser-<数字>` を使う）を新規登録し、ログイン・記事表示・記事のクリック・再読み込み・ログアウトができる。
3. 記事一覧の応答時間（API p95）を記録する。ブラウザの開発者ツールの Network タブで `/api/v1/feed` を20回読み込み、遅い方から2番目の値を p95 の目安として記録します（合否の基準は未設定のため、記録のみ。基準が決まっていなければ、結果だけで合格にしません）。
4. test 設定で Swagger UI が表示されることを確認する場合は、いったん `bash ops/rehearsal_browser.sh up test` で起動し直し、`http://127.0.0.1:18001/docs` を開く。

終わったら止めます。

```bash
bash ops/rehearsal_browser.sh stop
```

**ブラウザで作った合成の利用者**は、データベースに残ります。§6 の片付けまでの間、本番由来のデータと同じく扱います。片付けのときにDBごと扱うため、個別には消しません。

### 切り戻しの画面確認について

自動の工程（`rollback`）では、旧版の API を同じ隔離DBへ向けて起動し、稼働確認までを行います。旧版の画面（Cloudflare Pages の直前の公開）はこの環境では起動できないため、旧版の画面と旧版の API の組み合わせのブラウザ確認は、次のどちらかで行います。

- 旧版のソースから画面を作り（`VITE_API_BASE_URL=https://api.mytechpulse.net`）、`frontend-site` を差し替えて起動し直し、旧版 API とブラウザで確認する。
- 行わなかった場合は、記録に「未確認」と書き、判定者が合否を決める（未確認のまま自動で合格にしません）。

## 5. 合否の判定と記録

次をすべて満たしたとき、#126 のリハーサルを合格とします。

- 自動の検査（`rehearsal.sh`）とブラウザ確認がすべて成功した
- 件数・制約・採番の確認が成功し、既存データの比較に重大な差がない（不一致の表の数が 0）
- 主要な操作の確認（動作確認）が成功した
- 停止相当の工程が30分以内に終わった
- 切り戻しと、その後の稼働確認が成功した
- 暗号化したバックアップを使い、復号した一時ファイルが残っていない

次の表を埋めて、結果を #126 に記録します。Manifest SHA256 や API の識別子（digest）は「照合できたか」だけを書き、値は転記しません。nonce・データの要約値・生データ・秘密情報・暗号化パスワードは書きません。

| 記録項目 | 結果 |
| --- | --- |
| 実施日・作業者 |  |
| 判定者 |  |
| 事前承認の範囲 |  |
| Manifest SHA256 の照合（値は書かない） |  |
| 配布元の実行回の ID / attempt |  |
| API の完全な識別子の照合（値は書かない） |  |
| 件数・制約・採番の確認 |  |
| データ内容の一致（不一致の表の数） |  |
| 動作確認（`smoke`）と合成データの片付け |  |
| ブラウザ確認 |  |
| API p95（ミリ秒） |  |
| 停止相当の所要時間（分・秒） |  |
| 切り戻しの結果と所要時間（分・秒） |  |
| 旧版の画面の確認（実施 / 未確認） |  |
| 復号した一時ファイルの削除確認 |  |
| 判定（合格 / 不合格） |  |
| 中止した場合の概要（機密値を含めない） |  |

#127 へ渡す合格の証跡は、「Manifest SHA256 の照合結果」「配布元の実行回の ID / attempt」を一組として示します。1つでも満たさない、または未確認の項目がある場合は、#126 を open のままにし、#127 を始めません。

## 6. 片付け（#127 が終わって安定してから）

#126 に合格しても、切り戻しで同じバックアップと環境が再び必要になるため、#127 の安定確認が終わって Issue を close するまでは何も削除しません。close した後、次の内容をオーナーに示して承認を得てから、人間が行います。自動のスクリプトは削除をしません。

| 対象 | 役割 | 削除した場合の影響 |
| --- | --- | --- |
| 暗号化したバックアップ（`.gpg`）と、チェックサム | リハーサル用に保管した本番相当のデータ | 再リハーサルには新しいバックアップが要る。サーバーの `backups/` には触れない |
| `mytechpulse_rehearsal_db` ボリューム | 復元・移行を試した隔離DB（復元した本番相当データを含む） | 検証DBを復元し直す必要がある。本番のボリュームとは別名で、本番には影響しない |
| 作業フォルダの展開物（`releases/`、`archives/`）と一時フォルダ | 配布物の展開・復号の作業場所 | 配布物は再取得できる（保持期間内）。復号の一時フォルダに平文が残っていないか先に確認する |
| ブラウザ確認用の専用 profile のフォルダ | 隔離環境を開いたブラウザの記録 | なし（本番の記録は別） |

削除後、PC に本番由来のデータ（暗号化したバックアップ、検証DBのボリューム、復号用の一時フォルダ）が残っていないことを確認し、結果だけを Issue に記録します。承認がないとき、または対象を特定できないときは、削除せずオーナーに確認します。
