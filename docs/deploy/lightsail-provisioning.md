# AWS Lightsail プロビジョニング手順

MyTechPulse のバックエンド（FastAPI + PostgreSQL）を動かす AWS Lightsail インスタンスを用意する手順。
フロントエンドは Cloudflare Pages に分離する構成なので、このインスタンスは **API と DB のみ**を持つ。

構成の決定経緯は Issue #65（2分割の判断）と Issue #67（デプロイ先の確定）にある。

> このファイルは Lightsail のプロビジョニングに限定した部品。
> デプロイ全体の手順書（`DEPLOYMENT.md`）は Issue #55 で作成し、そこからこのファイルを参照する。

**Lightsail 側の仕様確認日: 2026-08-13**（料金・無料期間は変更されうるため、作業時に必ず現在値を確認する）

---

## 0. 前提と全体像

| 項目 | 値 |
|---|---|
| プラン | $7/月（1 GB RAM / 2 vCPU / 40 GB SSD / 転送 2 TB） |
| リージョン | 東京（`ap-northeast-1`） |
| OS イメージ | Ubuntu 24.04 LTS |
| 開放ポート | 22（SSH）/ 80（HTTP）/ 443（HTTPS） |

**東京は転送量が半減するリージョンに含まれない**ため、2 TB がフルで使える
（半減対象はムンバイ・シドニー・ジャカルタ・マレーシア・香港・サンパウロ）。

作業の流れ:

```
AWSアカウント作成 → 有料プランへ切替 → インスタンス作成（東京・1GB）
  → ファイアウォールで 80/443 開放 → SSH → スワップ2GB作成
  → ops/oracle-vm-setup.sh 実行 → 疎通確認
```

---

## 1. AWS アカウントの準備

### 有料プランへの切り替えは必須

**新規 AWS アカウントは、無料プランのままだと 6 ヶ月で閉鎖される。**
閉鎖されると本番環境ごと消えるため、**運用開始前に必ず有料プラン（Paid plan）へ切り替える**。

新規アカウントは対象プラン（$5 / $7 の Linux プラン）が **3 ヶ月無料**なので、
無料期間中に構築を進め、期間内に有料プランへ切り替える段取りにする。

その他:

- ルートユーザーで MFA を有効にする（このアカウントが本番の唯一の管理経路になる）
- 請求ダッシュボードで**予算アラート**を設定する（Lightsail は固定費だが、他サービスを触ったときの取りこぼしを防ぐ）
- 日常操作用に IAM ユーザーを作り、ルートユーザーを常用しない

## 2. SSH 鍵の準備

インスタンス作成時に公開鍵を登録する。ローカル PC 側で作っておく。

```bash
ssh-keygen -t ed25519 -C "mytechpulse-lightsail" -f ~/.ssh/mytechpulse_lightsail
```

秘密鍵（`~/.ssh/mytechpulse_lightsail`）は**リポジトリに絶対に入れない**。
GitHub Actions の自動デプロイ（Issue #54）では別途デプロイ用の鍵を作り、Secrets に登録する。

Lightsail はコンソールで鍵を自動生成する選択肢も出るが、**自分で作った鍵をアップロードする**ほうが扱いやすい
（自動生成鍵はダウンロード機会が一度きり）。

## 3. インスタンス作成

Lightsail コンソール → **Create instance**

1. **Instance location**: `Tokyo, Zone A`（`ap-northeast-1a`）を選ぶ
2. **Select a platform**: Linux/Unix
3. **Select a blueprint**: **OS Only → Ubuntu 24.04 LTS**
   （「Apps + OS」の LAMP 等は不要。Docker で全部立てるため）
4. **SSH key pair**: 手順 2 で作った**公開鍵**をアップロードする
5. **Choose your instance plan**: **$7/月（1 GB RAM / 2 vCPU / 40 GB SSD）**
6. **Identify your instance**: 名前を付けて Create

Oracle のような容量枯渇（Out of Host Capacity）との戦いは無く、通常は即座に作成できる。

### 静的 IP の割り当て（必須）

**インスタンス作成直後に静的 IP を割り当てる。** 既定のパブリック IP は**再起動で変わる**ため、
DNS を向けた後に再起動すると本番が落ちる。

Lightsail コンソール → Networking → **Create static IP** → 作成したインスタンスにアタッチ。

> 静的 IP は**インスタンスにアタッチされている限り無料**。デタッチしたまま放置すると課金対象になる。

## 4. ファイアウォールで ingress を開放

インスタンス → Networking タブ → **IPv4 Firewall** に追加する。

| アプリケーション | プロトコル | ポート | 用途 |
|---|---|---|---|
| SSH | TCP | 22 | 既定で開いている |
| HTTP | TCP | 80 | Let's Encrypt の HTTP-01 チャレンジに必須 |
| HTTPS | TCP | 443 | API 本番 |

SSH は「Restrict to IP address」で自分の IP に絞れるが、**固定回線でないなら絞らない**
（IP が変わると自分が締め出される）。SSH の防御は手順 6 のスクリプトが行う鍵認証強制と fail2ban で担保する。

> Oracle の Ubuntu イメージにあった「OS 側の `iptables` が全部 REJECT する」問題は **Lightsail には無い**。
> コンソールでポートを開ければそのまま通る。

## 5. スワップ領域の作成（必須）

**1 GB プランはメモリの余裕が薄い。** 実測見込みの内訳:

| 内容 | 使用量の目安 |
|---|---|
| Ubuntu + Docker 本体 | 約 200 MB |
| PostgreSQL | 約 200 MB |
| API（Python / uvicorn） | 約 150 MB |
| Caddy | 約 30 MB |
| **合計** | **約 580 MB / 1,024 MB** |

残り約 440 MB で、記事の日次バッチ取得（Issue #66）やイメージビルドのピークを吸収する必要がある。
**Lightsail の Ubuntu イメージにはスワップが設定されていない**ので、自分で作る。

SSH でログインして実行する（既定ユーザーは `ubuntu`）:

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

リポジトリを取得してセットアップスクリプトを実行する。

```bash
git clone https://github.com/H4aruki/MyTechPulse.git
cd MyTechPulse
sudo ./ops/oracle-vm-setup.sh
```

> **スクリプト名が `oracle-` のままなのは既知の負債。** 中身は Lightsail でもそのまま動く。
> Lightsail 向けへのリネームとスワップ処理の内包は `TASKS.md` の A 章に残してある。

スクリプトがやること（冪等。失敗したら直して再実行してよい）:

1. Docker Engine + Compose plugin の導入と `ubuntu` ユーザーの docker グループ追加
2. `iptables` に 80/443 の ACCEPT を追加（Lightsail では **REJECT ルールが無いため末尾に追加**され、実質無害）
3. SSH 硬化（パスワード認証・root ログインを無効化）
4. fail2ban の sshd jail 有効化
5. タイムゾーンを `Asia/Tokyo` に設定（`ops/backup_db.sh` の日次 cron を意図した時刻で回すため）

### Lightsail で読み替える点

- **「arm64 以外です」という警告が出るが無視してよい。** $7 プランは x86_64。
  Oracle の Ampere A1（ARM）向けに書かれたチェックが残っているだけで、処理は続行される
- **ARM64 でのビルド確認は不要になった。** `python:3.12-slim` も `postgres:17-alpine` も x86_64 で素直に動く

実行後、docker グループの反映のために**一度 SSH を切って再ログインする**。

## 7. アプリの起動（#52 の入口）

```bash
cp backend/.env.example backend/.env
# SECRET_KEY は本番用に新規生成する（開発用の値を使い回さない）
python3 -c "import secrets; print(secrets.token_hex(32))"
```

`backend/.env` に設定する値:

- `SECRET_KEY` — 上で生成した値
- `QIITA_ACCESS_TOKEN` — Qiita のアクセストークン
- `DATABASE_URL` — そのままでよい（`docker-compose.yml` が `host=db` に上書きする）
- `CORS_ALLOWED_ORIGINS` — Cloudflare Pages の URL が決まってから設定する（#53）

リポジトリルートの `.env` に DB のパスワードを設定する（既定値を本番で使わない）:

```bash
printf 'POSTGRES_PASSWORD=%s\n' "$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')" > .env
chmod 600 .env
```

> **前提**: PostgreSQL への移行（Issue #63）が済んでいること。
> 未了の場合、変数名は `MYSQL_ROOT_PASSWORD` のままである。

起動する（テーブル作成は `backend/entrypoint.sh` の `init_db.py` が自動実行する）:

```bash
docker compose up -d --build
docker compose logs -f api   # 起動ログを確認
```

全 API の疎通確認は Issue #52 で行う。

### メモリを実際に確認する

起動後、見込みどおりに収まっているかを必ず見る。

```bash
free -h                # 空きメモリとスワップ使用量
docker stats --no-stream   # コンテナごとの実使用量
```

**スワップを常時数百 MB 使っている状態は黄信号。** $12 の 2 GB プランへの移行を検討する
（スナップショットから任意のプランで作り直せる）。

## 8. 完了チェックリスト

- [ ] 静的 IP が割り当てられ、再起動しても IP が変わらない
- [ ] SSH でログインでき、パスワード認証が拒否される（`ssh -o PreferredAuthentications=password ubuntu@<IP>` が失敗する）
- [ ] `free -h` がスワップ 2 GB を返し、`sudo reboot` 後も残っている
- [ ] 再ログイン後、`docker run --rm hello-world` が **sudo なしで**成功する
- [ ] `docker compose version` が Compose v2 を返す
- [ ] ローカル PC から `nc -vz <IP> 22` / `80` / `443` が到達する
      （80/443 はまだ待受プロセスが無いので `Connection refused`。これは**到達している**証拠。
      ファイアウォールが閉じている場合はタイムアウトになる — この違いで切り分ける）
- [ ] `sudo fail2ban-client status sshd` が jail の稼働を返す
- [ ] `timedatectl` が JST を返す
- [ ] **AWS アカウントが有料プランに切り替わっている**（6 ヶ月での閉鎖を回避）

## 次のステップ

- **#51**: ドメイン取得（**課金を伴うためオーナー判断**）と Caddy による HTTPS 化
- **#52**: `docker-compose.yml` の本番起動と全エンドポイント疎通確認
- **#53**: Cloudflare Pages へのフロントエンドデプロイ
- **#54**: GitHub Actions による自動デプロイ
- **#55**: `DEPLOYMENT.md` への統合と移行ランブック

## 参考

- [Amazon Lightsail Pricing](https://aws.amazon.com/lightsail/pricing/)（プラン内容と転送量半減リージョン）
- [Create a static IP in Lightsail — AWS Docs](https://docs.aws.amazon.com/lightsail/latest/userguide/lightsail-create-static-ip.html)
- [Install Docker Engine on Ubuntu — Docker Docs](https://docs.docker.com/engine/install/ubuntu/)
