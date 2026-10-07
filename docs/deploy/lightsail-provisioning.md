# AWS Lightsail 構築手順（本番を一から作り直す）

MyTechPulse のバックエンド（Go製API・PostgreSQL・Caddy）を動かす AWS Lightsail のサーバーを、**一から用意する**手順です。画面（フロントエンド）は Cloudflare Pages に置くので、このサーバーが持つのは **API と DB と HTTPS の窓口だけ**です。

- 通常の更新（`main` に取り込んだ変更を本番へ出す）は、自動デプロイが行います。→ [go-auto-deploy.md](go-auto-deploy.md)
- この資料を使うのは、サーバーを作り直すとき（壊れた、引っ越す、など）だけです。
- 構成を決めた経緯は [ADR 0003](../adr/0003-host-on-cloudflare-pages-and-lightsail.md) にあります。
- データのバックアップと復元は [database-backup-and-restore.md](database-backup-and-restore.md) にあります。

**Lightsail 側の仕様確認日: 2026-08-13**（料金・無料期間は変わることがあるため、作業時に必ず現在の値を確認する）

---

## 0. 前提と全体像

| 項目 | 値 |
|---|---|
| プラン | $7/月（1 GB RAM / 2 vCPU / 40 GB SSD / 転送 2 TB） |
| リージョン | 東京（`ap-northeast-1`） |
| OS イメージ | Ubuntu 24.04 LTS |
| 開放ポート | 22（SSH）/ 80（HTTP）/ 443（HTTPS） |

**東京は転送量が半減するリージョンに含まれない**ため、2 TB がそのまま使える（半減対象はムンバイ・シドニー・ジャカルタ・マレーシア・香港・サンパウロ）。

作業の流れ:

```
AWSアカウント → 有料プラン → インスタンス作成（東京・1GB）→ 静的IP
  → ファイアウォール（80/443）→ SSH → スワップ2GB
  → ops/lightsail-vm-setup.sh → 設定ファイルと置き場所 → 最初のGo版の起動（初回だけの手作業）
  → 疎通確認 → 自動デプロイをつなぐ
```

---

## 1. AWS アカウントの準備

### 有料プランへの切り替えは必須

**新規 AWS アカウントは、無料プランのままだと 6 ヶ月で閉鎖される。** 閉鎖されると本番ごと消えるため、**運用開始前に必ず有料プラン（Paid plan）へ切り替える**。

新規アカウントは、対象プラン（$5 / $7 の Linux プラン）が **3 ヶ月無料**なので、無料期間中に構築を進め、期間内に有料プランへ切り替える。

そのほか:

- ルートユーザーで MFA を有効にする（このアカウントが本番の唯一の管理経路になる）
- 請求ダッシュボードで**予算アラート**を設定する
- 日常の操作用に IAM ユーザーを作り、ルートユーザーを常用しない

## 2. SSH 鍵の準備

インスタンス作成時に公開鍵を登録する。ローカル PC で作っておく。

```bash
ssh-keygen -t ed25519 -C "mytechpulse-lightsail" -f ~/.ssh/mytechpulse_lightsail
```

秘密鍵（`~/.ssh/mytechpulse_lightsail`）は**リポジトリに絶対に入れない**。自動デプロイ用の接続（`LIGHTSAIL_SSH_KEY`）は、これとは別に、デプロイ専用の鍵を作ってSecretsへ登録する（[ci-and-release.md](ci-and-release.md)の5章）。

Lightsail は鍵を自動生成する選択肢も出すが、**自分で作った鍵をアップロードする**ほうが扱いやすい（自動生成の鍵は、ダウンロードの機会が一度きり）。

## 3. インスタンス作成

Lightsail コンソール → **Create instance**

1. **Instance location**: `Tokyo, Zone A`（`ap-northeast-1a`）
2. **Select a platform**: Linux/Unix
3. **Select a blueprint**: **OS Only → Ubuntu 24.04 LTS**（「Apps + OS」は不要。Docker で全部立てるため）
4. **SSH key pair**: 手順 2 の**公開鍵**をアップロードする
5. **Choose your instance plan**: **$7/月（1 GB RAM / 2 vCPU / 40 GB SSD）**
6. **Identify your instance**: 名前を付けて Create

### 静的 IP の割り当て（必須）

**作成した直後に静的 IP を割り当てる。** 既定のパブリック IP は**再起動で変わる**ため、DNS を向けた後に再起動すると本番が落ちる。

Lightsail コンソール → Networking → **Create static IP** → 作成したインスタンスにアタッチ。

> 静的 IP は**インスタンスにアタッチされている間は無料**。外したまま放置すると課金対象になる。

## 4. ファイアウォールで ingress を開放

インスタンス → Networking タブ → **IPv4 Firewall** に追加する。

| アプリケーション | プロトコル | ポート | 用途 |
|---|---|---|---|
| SSH | TCP | 22 | 既定で開いている |
| HTTP | TCP | 80 | Let's Encrypt の証明書取得（HTTP-01）に必須 |
| HTTPS | TCP | 443 | API 本番 |

SSH は「Restrict to IP address」で自分の IP に絞れるが、**固定回線でないなら絞らない**（IP が変わると自分が締め出される）。SSH の防御は、手順 6 のスクリプトが行う鍵認証の強制と fail2ban で担保する。

## 5. スワップ領域の作成（必須）

**1 GB プランはメモリの余裕が薄い。** 見込みの内訳（Go版に切り替えた後の実測を反映）:

| 内容 | 使用量の目安 |
|---|---|
| Ubuntu + Docker 本体 | 約 200 MB |
| PostgreSQL | 約 200 MB |
| Go製API | 約 10 MB（切り替え後の実測は約 8 MB） |
| Caddy | 約 30 MB |
| **合計** | **約 450 MB / 1,024 MB** |

**Lightsail の Ubuntu イメージにはスワップが設定されていない**ので、自分で作る。SSH でログインして実行する（既定ユーザーは `ubuntu`）:

```bash
ssh -i ~/.ssh/mytechpulse_lightsail ubuntu@<静的IP>
```

```bash
sudo fallocate -l 2G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile

# 再起動後も有効にする
echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab

# ディスクへの退避を控えめにする（メモリが本当に足りないときだけ使う）
echo 'vm.swappiness=10' | sudo tee /etc/sysctl.d/99-swappiness.conf
sudo sysctl -p /etc/sysctl.d/99-swappiness.conf
```

確認:

```bash
free -h        # Swap の行に 2.0Gi が出る
swapon --show  # /swapfile が出る
```

## 6. VM の初期セットアップ

リポジトリを取得して、セットアップスクリプトを実行する。**このリポジトリの置き場所（`~/MyTechPulse`）は、自動デプロイが、バックアップ用のスクリプトを探す場所でもある**ので、この場所に置く。

```bash
git clone https://github.com/H4aruki/MyTechPulse.git
cd MyTechPulse
sudo ./ops/lightsail-vm-setup.sh
```

スクリプトがやること（何度実行しても同じ結果になる。失敗したら直して再実行してよい）:

1. Docker Engine + Compose plugin の導入と、`ubuntu` ユーザーの docker グループへの追加
2. `iptables` に 80/443 の ACCEPT を追加（Lightsail では REJECT ルールが無いため、末尾に追加される。実質は無害）
3. SSH の硬化（パスワード認証・root ログインを無効化）
4. fail2ban の sshd jail を有効化
5. タイムゾーンを `Asia/Tokyo` に設定（日次バックアップを意図した時刻で動かすため）

実行後、docker グループの反映のために、**一度 SSH を切って再ログインする**。

## 7. 設定ファイルと置き場所を用意する

自動デプロイが前提にする、サーバー上の場所は次の3つ。

| 場所 | 中身 | 用意する人 |
|---|---|---|
| `~/MyTechPulse` | リポジトリの複製。バックアップ用スクリプト（`ops/backup_db.sh`・`ops/verify_backup.sh`）の置き場所。手順 6 で作成済み | 手順 6 |
| `~/releases` | 配布物の置き場所。自動デプロイが、新しい版を、ここに実行回ごとに作る | 手作業（下記） |
| `~/mytechpulse-production.env` | 本番の設定ファイル | 手作業（下記） |

```bash
mkdir -p ~/releases
```

### 本番の設定ファイル（`~/mytechpulse-production.env`）

1行に1項目、`項目名=値` の形で書く。**値は、チャット・Issue・リポジトリに書かない。** 作ったら、自分だけが読めるようにする。

```bash
chmod 600 ~/mytechpulse-production.env
```

| 項目 | 値 | 補足 |
|---|---|---|
| `APP_ENV` | `production` | 本番にすると、Cookie名が `__Host-mtp_session`・`Secure` 付きになり、説明ページ（`/docs`）が閉じる |
| `POSTGRES_PASSWORD` | 新しく作ったランダムな値 | 開発用の値を使い回さない。`openssl rand -base64 24 \| tr -d '/+='` などで作る |
| `QIITA_ACCESS_TOKEN` | Qiita のアクセストークン | 未設定だと、APIは起動しない |
| `CORS_ALLOWED_ORIGINS` | 画面の公開URL（例: `https://mytechpulse.net`） | 複数ならカンマ区切り。`*` は使えない。画面のURLと完全に一致させる |
| `SWAGGER_ENABLED` | `false` | 本番では `true` にできない（起動に失敗する） |
| `API_DOMAIN` | APIのドメイン（例: `api.mytechpulse.net`） | Caddy が、このドメインの証明書を取る。DNS をこのサーバーの静的IPへ向けておく |

- 項目の意味と、ローカル開発との違いは、[`server/.env.example`](../../server/.env.example) と [`.env.example`](../../.env.example) のコメントにまとまっている。
- `docker-compose.yml` が API に渡す項目は、上の表のうち `APP_ENV`・`QIITA_ACCESS_TOKEN`・`CORS_ALLOWED_ORIGINS`・`SWAGGER_ENABLED` と、`POSTGRES_PASSWORD` から作る接続先。セッションの有効期間などの調整項目は、渡しておらず、既定値で動く。

## 8. 最初のGo版を起動する（初回だけの手作業）

自動デプロイは、**すでに動いているGo版の箱**を「戻し先」として記録してから入れ替える。そのため、**最初の1回だけは、手で起動する**必要がある。2回目以降は、自動デプロイが行う。

### 8-1. 起動する箱を決める

起動する箱は、tagではなく、**digest 付きの名前**で指定する（`ghcr.io/h4aruki/mytechpulse-api-go@sha256:…`）。入手先は、配布物を作った実行回（GitHub Actions の `release.yml`）の結果ページにある manifest。作り方は [ci-and-release.md](ci-and-release.md) を参照。

配布物をまだ作っていなければ、`main` を対象に、`release.yml` を手動で実行して作る。

### 8-2. データベースを起動し、構造を作る

8-2 から 8-4 は、同じターミナルで続けて実行する（`GO_API_IMAGE` の設定を引き継ぐため）。

```bash
cd ~/MyTechPulse
docker compose --env-file ~/mytechpulse-production.env -p mytechpulse up -d db
export GO_API_IMAGE='ghcr.io/h4aruki/mytechpulse-api-go@sha256:…'   # 8-1 で決めたもの
docker compose --env-file ~/mytechpulse-production.env -p mytechpulse --profile go-migrate run --rm migrate-go
```

- 空のデータベースなら、`server/db/migrations/` の変更が順に適用され、表ができる。
- 以前のデータを引き継ぐなら、このあいだに復元が要る。復元の手順は [database-backup-and-restore.md](database-backup-and-restore.md) にあるが、**復元先は新しい名前のDBに限られ**、本番のDBへ置き換える手順は、この資料には無い。置き換えが要るときは、オーナーと、その場で手順を決める。

### 8-3. API と HTTPS の窓口を起動する

```bash
docker compose --env-file ~/mytechpulse-production.env -p mytechpulse --profile go-preview --profile prod up -d api-go caddy
```

### 8-4. 動作を確認する

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8001/health/ready    # 200 が出る
curl -s -o /dev/null -w '%{http_code}\n' https://<API_DOMAIN>/health/ready     # 200 が出る
docker ps --format '{{.Names}}\t{{.Image}}'
```

- 1つ目はサーバー内から、2つ目は窓口（Caddy）経由。2つ目は、証明書の取得に少し時間がかかることがある。
- `docker ps` で、API の箱が `…@sha256:…` の形で動いていること（自動デプロイは、この形でないと「戻し先」にしない）。

### メモリを実際に確認する

```bash
free -h                    # 空きメモリとスワップ使用量
docker stats --no-stream   # コンテナごとの実使用量
```

**スワップを常時数百 MB 使っている状態は黄信号。** $12 の 2 GB プランへの移行を検討する（スナップショットから、任意のプランで作り直せる）。

## 9. 日次バックアップ

`ops/backup_db.sh` は、バックアップを1つ作って、中身を確認する。**毎日自動で動かすには、cron への登録が要る**（スクリプトの冒頭に、登録の例がある）。

- 取得と検証に成功した後、**7日を超えた古い世代が自動で削除される**。残したいバックアップには、同じ名前に `.keep` を付けた空のファイルを置く（[database-backup-and-restore.md](database-backup-and-restore.md)）。ディスクの空きは、ときどき確認する（`df -h`）。
- この動きは、サーバーの `~/MyTechPulse` が、`ops/prune_backups.sh` を含む新しい版であることが前提（`cd ~/MyTechPulse && git pull --ff-only`）。
- バックアップは、同じサーバーに保存される。サーバーごと失われると復元できないため、外部への退避が残っている（`TASKS.md`）。

## 10. 画面と自動デプロイをつなぐ

- **画面**: Cloudflare Pages へ公開する。自動デプロイが、APIの入れ替えが成功した後に、公開する（[go-auto-deploy.md](go-auto-deploy.md)）。接続用のSecretsは、[ci-and-release.md](ci-and-release.md)の5章を参照。
- **自動デプロイ**: リポジトリ変数 `GO_DEPLOY_ENABLED` を `true` にしたときから動く。有効にする前の確認は、[go-auto-deploy.md](go-auto-deploy.md)の3章。

## 11. 完了チェックリスト

- [ ] 静的 IP が割り当てられ、再起動しても IP が変わらない
- [ ] SSH でログインでき、パスワード認証が拒否される（`ssh -o PreferredAuthentications=password ubuntu@<IP>` が失敗する）
- [ ] `free -h` がスワップ 2 GB を返し、`sudo reboot` の後も残っている
- [ ] 再ログイン後、`docker run --rm hello-world` が **sudo なしで**成功する
- [ ] ローカル PC から `nc -vz <IP> 22` / `80` / `443` が到達する（80/443 は、待ち受けが起動する前なら `Connection refused`。これは**到達している**証拠。ファイアウォールが閉じていると、タイムアウトになる。この違いで切り分ける）
- [ ] `sudo fail2ban-client status sshd` が jail の稼働を返す
- [ ] `timedatectl` が JST を返す
- [ ] `~/mytechpulse-production.env` の権限が `600` で、必要な6項目がある
- [ ] 8-4 の確認で、`/health/ready` が、サーバー内と窓口経由の両方で 200 を返す
- [ ] ブラウザで、会員登録・ログイン・記事の表示ができる
- [ ] 日次バックアップが cron に登録され、1回は実行されて、検証（`ops/verify_backup.sh`）が通っている
- [ ] **AWS アカウントが有料プランに切り替わっている**（6 ヶ月での閉鎖を避ける）

## 参考

- [Amazon Lightsail Pricing](https://aws.amazon.com/lightsail/pricing/)（プランの内容と、転送量が半減するリージョン）
- [Create a static IP in Lightsail — AWS Docs](https://docs.aws.amazon.com/lightsail/latest/userguide/lightsail-create-static-ip.html)
- [Install Docker Engine on Ubuntu — Docker Docs](https://docs.docker.com/engine/install/ubuntu/)
