# 本番のGo版への切り替え手順

本番のAPIを、Python版からGo版へ入れ替えます。#127 の手順です。本番には既存の利用者のデータがあるため、停止（5〜30分）と切り戻しの条件を決めたうえで行うメンテナンス作業として実施します。

- **実行はオーナーが本番サーバーで手動で行います**（2026-10-06 決定）。AI は本番サーバーに接続しません。この文書のコマンドは、そのままコピーして使えるようにしてあります。
- 段階ごとに1つずつ実行し、前の段階が成功しないと次の段階は実行できません（順番違いは、スクリプトが止めます）。
- 実施日時、停止の案内、承認は、オーナーが決めます。

## 1. 何を切り替えるか

接続先のホスト名（`api.mytechpulse.net`）は変えません。同じ名前の裏側で動くものを、画面とセットで入れ替えます。

| 部品 | いま | 切り替え後 | 場所 |
| --- | --- | --- | --- |
| API | Python版 | Go版（配布情報で固定した箱） | 本番サーバー（Lightsail） |
| データベース | 3表（`user`、`tag`、`recommend`） | 3表はそのまま。権限の列と、ログイン用の表（`auth_session`）を**追加するだけ** | 本番サーバー |
| 窓口（Caddy）の向け先 | Python版（`api:8000`） | Go版（`api-go:8001`） | 本番サーバー |
| 画面 | 旧版（トークン方式） | 新版（Cookie方式。Go版の窓口に合わせて作り直し済み） | Cloudflare Pages |

- **画面とAPIは必ずセットで切り替えます。** 古い画面と新しいAPI、新しい画面と古いAPIの組み合わせは動きません。切り戻しも、画面・API・運用ファイルの組で戻します。
- データベースの変更は追加だけで、Python版はそのまま使えます。切り戻しで、利用者のデータは巻き戻しません。
- ログインの仕組みが変わるため、**切り替え後は、利用者全員が一度ログインし直す**ことになります（旧版のトークンは使えなくなります）。パスワードは変わりません。旧版が作った形式のパスワードを、Go版が読めること（#126）と、Go版が作る形式を、Python版が読めること（切り戻しのため）を、どちらも確認済みです。
- 古い画面を開いたままのブラウザは、Go版の窓口に届かず、エラーになります。**再読み込み（強制再読み込み）で新しい画面になります。**

## 2. 前提（#126 の合格の証跡）

- #126 は合成データで合格（2026-10-06・オーナー判定）。
- 切り替えに使う成果物は、**配布元の実行回（run）で作った3つ**（API の箱・画面・運用ファイル）と manifest だけです。本番サーバーで再 build しません。実際の切り替えには、その時点の `main` から新しく配布を実行し、`docs/deploy/go-migration-rehearsal.md` §2-2・§2-3 と同じ手順（箱の中身の確認、manifest の照合）で確かめた実行回を使います。
- 合成データでは確かめられなかったこと（実データにだけある変わった値）は、§4-1 の事前の確認と、§5 の移行前後の内容比較で補います。

## 3. 部品と、確認の状況

| 部品 | 内容 | 場所 | 確認 |
| --- | --- | --- | --- |
| 窓口の向け先の切り替え | Python版・メンテナンス応答（503）・Go版の3種類を選べる | `ops/caddy/`、`docker-compose.yml`（`MTP_CADDYFILE`） | 本物のCaddyで確認済み（`ops/cutover_caddy_test.sh`） |
| 切り替え・切り戻しの実行 | 段階ごとのスクリプト | `ops/cutover.sh` | 偽の道具で36項目（`ops/cutover_test.sh`）。**本物のDocker・Caddy・PostgreSQL・Go版の箱で、本番と同じ構成を別の名前・別のportに作り、切り替えから切り戻しまで通した**（`ops/cutover_integration_test.sh`） |
| Go版のDB移行 | 箱の `/migrate` を1回実行 | `docker-compose.yml`（`migrate-go`） | 上の通しで確認済み |
| 前後の内容比較・動作確認・片付け | #126 で使ったものと同じ | `ops/snapshot_migration_state.sh`、`ops/compare_migration_state.sh`、`ops/rehearsal_smoke.sh`、`ops/sql/rehearsal_cleanup.sql` | #126 で確認済み |
| 画面の公開と切り戻し | Cloudflare Pages（手動） | §5 の段階6、§6 | **未確認**（実際のアカウントでの操作は、当日が初めて） |

この環境では確認できないこと（当日、サーバー上で確認が要る）:

- 本物のドメインでのHTTPSと証明書（Caddy の設定を切り替えても、保存済みの証明書をそのまま使う設計）
- 本番サーバーのメモリ・ディスクの余裕（1GBのプラン）
- Cloudflare Pages の操作
- 本番の実データ（§4-1 で事前に確認）

## 4. 事前準備（切り替え日までに）

サーバーで実行するコマンドは、サーバーにログインして実行します。

### 4-0. 守ること

- いまの本番の運用ファイルがある場所（`~/MyTechPulse`）は、**切り戻し先として使うため、切り替えが終わって安定するまで更新・移動・削除しません。** `git pull` もしません。
- compose のプロジェクト名は `mytechpulse` のまま使います（保存場所と同じ名前を使うため）。

### 4-1. 本番データの事前確認（読み取りだけ）

合成データでは確かめられなかった、実データの変わった値を、先に見つけておきます。`~/MyTechPulse` で実行します。

1. タグの衝突の監査（大文字・小文字や前後の空白だけが違うタグが重複していないか）。衝突があると、Go版の登録・タグ処理が正しく動きません。
   ```bash
   docker compose exec -T db psql -X -q -v ON_ERROR_STOP=1 -U postgres -d mytechpulse < ops/sql/audit_tag_collisions.sql
   ```
   `0` なら続行。1以上、または実行できなければ**中止**します。衝突したタグは自動で統合せず、人が決めて、承認を得てから直します。
2. 利用者名が50文字以内であること（Go版の入力の上限）の確認（読み取りのSQL）。
   ```bash
   docker compose exec -T db psql -X -q -At -U postgres -d mytechpulse -c "SELECT count(*) FROM \"user\" WHERE char_length(user_name) > 50 OR user_name <> btrim(user_name)"
   ```
   `0` なら続行。

### 4-2. Go版の設定ファイル（オーナーが作る。AIは値を読みません）

Git管理の外に、設定ファイルを1つ作ります（例: `~/mytechpulse-production.env`、権限は `600`）。書く項目は次のとおりです（値は、`server/.env.example` と、いまの本番の設定を見て決めます）。

| 項目 | 値 |
| --- | --- |
| `POSTGRES_PASSWORD` | いまの本番DBのパスワードと**同じ**もの（Go版がこの値でDBへ繋ぎます。`preflight` が実際に繋がるか確認します） |
| `API_DOMAIN` | `api.mytechpulse.net` |
| `APP_ENV` | `production`（必須） |
| `QIITA_ACCESS_TOKEN` | 本物のQiitaのトークン |
| `CORS_ALLOWED_ORIGINS` | 本番の画面のオリジン（カンマ区切り）。例 `https://mytechpulse.net,https://www.mytechpulse.net`。Cookie方式のため、APIと同じサイト（`mytechpulse.net`）で配信されている必要があります |
| `SWAGGER_ENABLED` | `false`（必須） |

```bash
chmod 600 ~/mytechpulse-production.env
```

### 4-3. 切り戻し用の記録（`previous-release.json`）

切り戻しは、この記録にある旧版の運用ファイルの場所を使います。形式は `ops/deploy_release.sh` の冒頭の説明と、`docs/deploy/go-migration-rehearsal.md` §2-5 のとおりです。

- `api_image`: いま動いているPython版の箱の識別子（`docker compose ps` / `docker image ls` で確認。ふだん使っている `mytechpulse-api`）
- `ops_release_dir`: `~/MyTechPulse` の**絶対パス**（`/home/<ユーザー名>/MyTechPulse`）
- `frontend_deployment_id`: Cloudflare Pages の、いまの公開の識別子（ダッシュボードの Deployments で確認して控える）
- 残りの項目は、記録の書式を満たす値（例 `legacy-python-no-manifest`）でよい（値は `. _ : / @ + = -` と英数字だけ）

作った記録を `MTP_PREVIOUS_RELEASE_RECORD` で渡します（次の 4-4）。

### 4-4. 配布と検証、サーバーへの準備

1. 配布を実行する（`release.yml`。外部への送信にあたるので、オーナーの許可が要る）。
2. `docs/deploy/go-migration-rehearsal.md` §2-2、§2-3 と同じ手順で、箱の中身を確認し、manifest の SHA256 を照合する。
3. サーバーで、配布物を取り出し、`ops/deploy_release.sh` で準備する（**公開はまだ切り替わらない**）。
   ```bash
   export MTP_RELEASE_MANIFEST=<manifestのpath>
   export MTP_MANIFEST_SHA256=<ActionsのSummaryに出たManifest SHA256>
   export MTP_RELEASE_RUN_ID=<実行回のID>
   export MTP_RELEASE_RUN_ATTEMPT=<attempt>
   export MTP_RELEASES_ROOT=/home/<ユーザー名>/releases
   export MTP_PREVIOUS_RELEASE_RECORD=/home/<ユーザー名>/previous-release.json
   bash <ops のarchiveを展開した場所>/ops/deploy_release.sh <3つの成果物を置いたフォルダ>
   ```
   成功すると、`$MTP_RELEASES_ROOT/<コミットSHA>-<実行回ID>-<attempt>/` ができ、中に運用ファイル一式、`release.env`、`previous-release.json` が入ります。このフォルダを、以降 **`MTP_RELEASE_DIR`** と呼びます。

### 4-5. 決めること・知らせること

- 実施日時（利用が少ない時間帯）と、作業者・承認者。
- 利用者への案内: メンテナンスの日時と、**切り替え後にログインし直すこと**。案内の方法（画面に出す、SNS、など）は未定。
- 中止条件（§5 の表と §6）と、切り戻しを決める人。
- 監視の期間（案: 72時間）。

### 4-6. 練習（任意だが推奨）

実際の切り替えの前に、手元のPCで、本番と同じ構成を別の名前・別のportで作って通せます（本番には触れません）。

```bash
MTP_IT_GO_IMAGE=ghcr.io/h4aruki/mytechpulse-api-go@sha256:<digest> MTP_IT_LEGACY_IMAGE=mytechpulse-api:latest \
  bash ops/cutover_integration_test.sh
```

## 5. 当日の手順

役割: 作業者（実行）、承認者（各段階の承認）、記録係（時刻と結果を #127 に記録）。

最初に、サーバーで次を設定します（同じ画面の中で、全段階を実行します）。

```bash
export MTP_RELEASE_DIR=/home/<ユーザー名>/releases/<コミットSHA>-<実行回ID>-<attempt>
export MTP_ENV_FILE=/home/<ユーザー名>/mytechpulse-production.env
export MTP_CUTOVER_ORIGIN=https://mytechpulse.net   # 本番の画面のオリジン（CORS_ALLOWED_ORIGINS に含まれること）
cutover() { bash "$MTP_RELEASE_DIR/ops/cutover.sh" "$@"; }
```

各段階は、`cutover <段階>` で実行します。出力は、段階の名前・秒数・成功/失敗と、固定の文だけです（設定ファイルの値は出ません）。**失敗したら、次へ進まず、止まって記録係に伝え、§6 の判断をします。** 状態は、いつでも `cutover status` で見られます。

| 段階 | コマンド | 何をするか・確認すること | 失敗したとき |
| --- | --- | --- | --- |
| 0. 事前確認 | `cutover preflight` | 何も変えない確認。設定ファイル、箱、DBパスワード、Caddyの向け先、空き容量。何度でも実行できる | 原因を直す。開始しない |
| 1. 開始 | — | 作業者・承認者・実行回・中止条件を確認し、承認者が開始を承認 | 開始しない |
| 2. メンテナンス | `cutover maintenance-on` | Caddyを503の応答にし、Python版APIを止める。**ここから停止時間が始まる** | `rollback` |
| 3. バックアップ | `cutover backup` | 最終バックアップを取り、チェックサムと読み取りを確認。件数（利用者,タグ,興味度）が出るので記録 | **中止**（`rollback`） |
| 4. 移行前の記録 | `cutover snapshot-before` | 移行前のDBの状態を記録する（内容は表に出ない） | 中止（`rollback`） |
| 5. 移行 | `cutover migrate` | Go版のDBマイグレーションを1回実行（追加のみ） | 中止（`rollback`。DBは追加分だけの状態で、Python版は動く） |
| 6. 比較 | `cutover compare` | 移行後の状態を記録して比較。一致、かつ件数が同じ | **中止して `rollback`** |
| 7. Go版の起動 | `cutover go-start` | Go版を起動し、稼働確認。まだ公開しない | `rollback` |
| 8. 画面の公開 | （手動） | 下の「画面の公開」を実行 | `rollback` |
| 9. 切り替え | `MTP_CUTOVER_CONFIRM_FRONTEND=yes cutover switch` | CaddyをGo版へ向ける。**ここで停止が終わる。** 停止時間が出る | `rollback` |
| 10. 動作確認 | `cutover smoke` | 本番のホスト名で、登録→本人確認→記事一覧→クリック→ログアウト。続けて、**ブラウザで**既存の利用者のログインと記事表示、**Qiitaの記事が取れること**を確認する | `rollback` |
| 11. 片付け | `cutover smoke-cleanup` | smokeが作った合成利用者と、その関連行だけを消す | 原因を調べる（消す対象は合成利用者のみ） |
| 12. 完了 | `cutover finish` | 記録用の一時ファイル（nonce・snapshot）を消す。バックアップは消さない | — |

### 画面の公開（段階8・手動）

新しい画面を、再 build せずに、同じ実行回の画面の成果物（`frontend-<コミットSHA>`）から公開します。自分のPCで行います。

1. 同じ実行回から `frontend-<コミットSHA>` を取り出して展開する（`gh run download <実行回ID> -R H4aruki/MyTechPulse -n frontend-<コミットSHA>` の後、`tar -xzf` で展開）。
2. Cloudflare Pages へ公開する（**要確認**: 実際のアカウントでの操作は、当日が初めてです）。
   ```bash
   npx wrangler pages deploy <展開したフォルダ> --project-name=mytechpulse --branch=main
   ```
3. 公開できたら、Cloudflare Pages の Deployments で、新しい公開が現在のものになっていることを確認する。

画面を公開してから、段階9（`switch`）を実行します（`MTP_CUTOVER_CONFIRM_FRONTEND=yes` を付けるのは、この確認の意味です）。

### 停止時間の目安

- #126 の合成データでは、停止相当が11秒でした。実データの量や、サーバーの余裕で、もっと長くなります。
- 段階2〜9（`maintenance-on` から `switch`）が30分を超えそうなら、切り戻しを検討します。

## 6. 切り戻し

### 切り戻しを決める条件

- 段階3〜10のどれかが失敗した
- 切り替え後、利用者が使えない状態が続く（ログインできない、記事が一切出ない、など）
- 停止時間が30分を超えそう

決める人: 承認者（オーナー）。

### 手順

1. サーバーで次を実行する（途中の段階でも、いつでも実行できる）。
   ```bash
   cutover rollback
   ```
   これは、次を順に行います。Caddyをメンテナンスにする → Go版を止める（コンテナを消さない）→ `previous-release.json` にある旧版の運用ファイルで、Python版APIを起動する → 稼働確認 → Caddyを旧版の設定（Python版向け）へ戻す → 本番のホスト名で確認。**データベースは巻き戻しません。**
2. 画面を、直前の公開へ戻す（Cloudflare Pages の Deployments で、控えておいた識別子の公開を選んで「Rollback」）。`rollback` の最後に、その識別子が表示されます。
3. 稼働確認（Python版でログイン、記事一覧）。
4. 状態は、名前を変えて残ります（`cutover-state.rolledback-…`）。最初の段階（`preflight`）から、やり直せます。

- Go版が追加した列と表は、Python版が無視します。切り替え中にGo版で受け付けた書き込みも失いません（Go版で登録した利用者も、Python版で同じパスワードでログインできます。Go版が作る形式 `$2a$10$…` を、Python版の照合処理で読めることを確認済み）。
- 破壊的な `down` のマイグレーションは使いません。バックアップの復元（`pg_restore`）も、切り戻しには使いません。
- `rollback` の一部が失敗した場合は、失敗した工程が表示されるので、その工程を手で続けます（`cutover status` で状態を確認）。

## 7. 切り替え後の監視（案: 72時間）

- 1時間後、翌日、72時間後に、エラー、サーバーの余裕（メモリ・ディスク。1GBのプランです）、記事一覧の応答、ログインの成否を確認し、#127 に結果だけを記録する。
- 異常があれば §6 の判断。
- 安定を確認したら、#128（Python版の削除）の議論に進む。削除は、対象・役割・影響を示してオーナーの許可を得てから行う。`~/MyTechPulse`（切り戻し先）と、バックアップも、その時点まで残す。

## 8. 記録

秘密の値、パスワード、バックアップの中身は書きません。

| 記録項目 | 結果 |
| --- | --- |
| 実施日時・作業者・承認者 |  |
| 配布元の実行回 ID / attempt、Manifest SHA256 の照合 |  |
| 切り戻し用の記録（Python版の識別子、画面の直前の公開の識別子） |  |
| 最終バックアップ（ファイル名、チェックサム一致、件数） |  |
| 移行前後の内容比較（一致、件数） |  |
| 停止時間（`switch` で表示された秒数） |  |
| 動作確認（`smoke`）・ブラウザ確認 |  |
| Qiitaの記事が取れること |  |
| 監視の結果（1時間後、翌日、72時間後） |  |
| 切り戻しの有無と理由 |  |

## 9. オーナーに決めてほしいこと

1. 実施の日時と、利用者への案内の方法・文面。
2. 承認者。
3. 本番の画面のオリジン（`CORS_ALLOWED_ORIGINS`）。`mytechpulse.net`、`www.mytechpulse.net`、`pages.dev` のうち、実際に使うもの。
4. 監視の期間。
5. 配布の実行（`release.yml`）。外部への送信にあたるので、実行前に許可が要る。
