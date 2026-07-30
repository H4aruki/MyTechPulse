# Oracle Cloud Always Free VM プロビジョニング手順

MyTechPulse のバックエンド（FastAPI + MySQL）を動かす Oracle Cloud Always Free VM を用意する手順。
Issue #50 に対応する。フロントエンドは Cloudflare Pages に分離する構成なので、この VM は API と DB のみを持つ。

> このファイルは Oracle VM のプロビジョニングに限定した部品。
> デプロイ全体の手順書（`DEPLOYMENT.md`）は Issue #55 で作成し、そこからこのファイルを参照する。

**Oracle 側の仕様確認日: 2026-07-30**（Oracle は無料枠を無告知で変更した実績があるため、作業時に必ず現在値を確認する）

---

## 0. 前提と全体像

| 項目 | 値 |
|---|---|
| シェイプ | `VM.Standard.A1.Flex`（Ampere A1 / ARM） |
| Always Free 枠 | **2 OCPU / 12 GB RAM**（月 1,500 OCPU時間 + 9,000 GB時間） |
| OS イメージ | Canonical Ubuntu 24.04（**aarch64** 版） |
| 開放ポート | 22（SSH）/ 80（HTTP）/ 443（HTTPS） |

Always Free の Ampere A1 枠は 2026-06-15 に 4 OCPU / 24 GB から **半分に縮小**された（Oracle からの告知は無く、ドキュメントが差し替わっただけ）。
枠の縮小・廃止リスクは許容した上で採用しており、限界に達した場合は さくら VPS 等へ移行する（判断基準と移行手順は Issue #55 の `DEPLOYMENT.md` に記載する）。

作業の流れ:

```
アカウント作成（リージョン・アカウント名は変更不可） → コンパートメント作成
  → インスタンス作成（容量エラーと戦う） → Security List で 22/80/443 開放
  → SSH → ops/oracle-vm-setup.sh 実行 → 疎通確認
```

---

## 1. アカウント作成（後から変更できない2つの決定）

サインアップ画面で決める項目のうち、**ホームリージョン**と**クラウド・アカウント名**は後から変更できない。
しかも Always Free アカウントは**1人1つまで**（複数作成は規約違反でアカウント停止対象）なので、
「作り直す」という逃げ道が実質的に無い。この2つは先に決めてから申し込む。

サインアップ先: <https://signup.oraclecloud.com>

### 1-1. ホームリージョン

Always Free のコンピュート・インスタンスは**ホームリージョンにしか作成できない**。

| リージョン | 利点 | 欠点 |
|---|---|---|
| `ap-tokyo-1` / `ap-osaka-1` | 日本のユーザーからのレイテンシが小さい | Ampere A1 の空き容量が枯渇しがちで「Out of Host Capacity」に遭いやすい |
| 米国・欧州リージョン | A1 の空きを見つけやすい | 日本からの往復レイテンシが乗る（API 応答が体感で遅くなる） |

**MyTechPulse の方針: まず `ap-tokyo-1`（Japan East (Tokyo)）を狙う。** 記事一覧の取得が体感速度に直結するアプリなので、レイテンシを優先する。
どうしても容量が確保できない場合に限り、手順 3 のリトライ手段を尽くした上で他リージョンを検討する。
その場合は無料アカウントを2つ持つことになる（規約違反）のを避けるため、
[既存の Free Tier テナンシーを削除する公式手順](https://docs.oracle.com/iaas/Content/General/Tasks/deleting_tenancy_freetier.htm)を踏んでから作り直す。

### 1-2. クラウド・アカウント名（= テナンシー名）

**プロダクト名を付けてはいけない。** `mytechpulse` のような名前にすると、将来まったく別のプロダクトで
Oracle を使うときもその配下に入ってしまう。

- クラウド・アカウント名はそのまま**テナンシー名**になり、コンソールのサインイン識別子として使われる
- **Always Free のテナンシーはリネームできない**（リネームには有料サブスクリプションが必要）
- 前述のとおり無料アカウントは1人1つなので、プロダクトごとにアカウントを分けることもできない

したがって `haruki-cloud` のような**個人名義の中立な名前**にする。
サインイン識別子なのでグローバルで一意である必要があり、短い一般的な名前は既に使われていて弾かれることがある。

### 1-3. プロダクトの分離はコンパートメントで行う

テナンシー1つに複数プロダクトが同居する前提なので、分離はコンパートメントで行う。
コンパートメントは後から作成・リネーム・リソースの移動ができ、Always Free 枠はテナンシー単位で計上されるため、
どのコンパートメントに置いても無料のままである。

```
テナンシー: haruki-cloud            ← アカウント名（変更不可）
├── コンパートメント: mytechpulse    ← 本プロジェクトのVM / VCN をここに作る
├── コンパートメント: <別プロダクト>  ← 将来必要になったら足す
└── コンパートメント: sandbox        ← 検証用
```

アカウント作成後、インスタンスを作る前に **Identity → Compartments** で `mytechpulse` コンパートメントを作成しておく。

### 1-4. その他の注意点

- クレジットカード登録を求められる。**検証用に少額の一時オーソリが立つことがある**（自動で解放される）
- Always Free の範囲では課金されないが、**「アップグレード」を促す表示に応じない**（従量課金アカウントに変わる）
- 最初の30日は US$300 のフリートライアルが付く。インスタンスを「Always Free eligible」な構成で作れば、トライアル終了後もそのまま残る
- MFA を有効にする（このアカウントが本番の唯一の管理経路になる）

### 1-5. サインアップが完了できない場合

Oracle は原因を示さない汎用エラーを返す（本プロジェクトでも 2026-07-30 に遭遇）。

> サインアップを完了できません。…a) 不完全または不正確な情報を入力しています。
> b) 意図的に、または意図せずに所在地またはアイデンティティが隠蔽されています。
> c) 複数のアカウントを作成しようとしています。

**この3つの理由は実際の原因とほぼ無関係**なので、以下を効く順に潰す。

1. **カードの種類** — 日本のユーザーが最も踏む。**JCB は通らない**という報告が多数（Visa / Mastercard / Amex を使う）。
   プリペイド・バーチャルカードは不可。海外取引が有効な**物理のクレジットカード**を使う
2. **回線** — VPN・プロキシ・DNS フィルタを全部オフ。学校や職場のネットワークも避ける
   （IP が匿名扱いされると b) の「所在地の隠蔽」判定になる）
3. **ブラウザ** — シークレットウィンドウ＋拡張機能を全て無効化。Cookie とキャッシュをクリア（広告ブロッカーが原因のことがある）
4. **入力内容** — 住所は**カードの請求先住所と完全一致**、かつ**ローマ字**で入力する。
   電話番号は国番号 `+81` を選び先頭の `0` を落とす
5. **リトライ間隔** — 同一メール・同一カードで連続失敗するとフラグが立って通らなくなる。
   **24時間空けて、一度も Oracle に使っていないメールアドレス**で試す
6. **モバイル回線＋スマホのブラウザから登録** — 自宅回線の IP が過去に他アカウントで使われている場合に効く

#### 問い合わせ先（導線を間違えると何も起きない）

**Free Tier アカウントはサポートリクエスト（SR）を起票できない**
（[Oracle Docs](https://docs.oracle.com/iaas/Content/GSG/Tasks/contactingsupport.htm): *"Support requests are available to paid accounts only"*）。
一般の問い合わせ窓口に投げるとこの規定で終わるため、**ライブチャットが唯一の公式窓口**になる。

```
https://www.oracle.com/corporate/contact/
  → Chat with Oracle Sales の「Open a live chat」
  → チャットボットで「Get Support」
  → 「Cloud Infrastructure including Free Trial」
  → 「Cloud Support Chat」
```

営業チャットで止まると「アカウントを作ってください」で終わるので、**Cloud Support Chat まで進む**。
依頼内容は「アカウント作成リクエストの**手動レビュー（manual review）**」と明示する。
埒が明かない場合の第2ルートは [Cloud Customer Connect](https://community.oracle.com/customerconnect/) への投稿
（公開の場なのでカード情報・住所・電話番号は書かない）。

**カード番号・セキュリティコード・パスワードは絶対に伝えない。** 正規のサポートがこれらを聞くことはない。

#### 長期化した場合

問い合わせの返答は数日〜1週間単位で読めない。既存の `docker-compose.yml` は**どの VPS でもそのまま動く**ため、
Issue #55 に記載予定の「フェーズB = 国内VPS へ移行」を、**フェーズAを飛ばして最初からBで始める**選択肢がある。
#51（Caddy/HTTPS）・#54（Actions デプロイ）の作業内容はほぼ同一で、x86 の VPS なら
**#52 の ARM64 動作確認が丸ごと不要**になる。VPS 契約は課金判断なのでオーナー確認が必要。

## 2. SSH 鍵の準備

インスタンス作成時に公開鍵を登録する。ローカル PC 側で作っておく。

```bash
ssh-keygen -t ed25519 -C "mytechpulse-oracle" -f ~/.ssh/mytechpulse_oracle
```

秘密鍵（`~/.ssh/mytechpulse_oracle`）は**リポジトリに絶対に入れない**。
GitHub Actions の自動デプロイ（Issue #54）では別途デプロイ用の鍵を作り、Secrets に登録する。

## 3. インスタンス作成

コンソール → Compute → Instances → **Create instance**

0. **Compartment**: 手順 1-3 で作った `mytechpulse` を選ぶ（既定の root コンパートメントのままにしない）
1. **Image and shape** を変更する
   - Image: Canonical Ubuntu 24.04（**aarch64** と付いているものを選ぶ。付いていないものは amd64）
   - Shape: Ampere → `VM.Standard.A1.Flex` → OCPU `2` / メモリ `12` GB
   - 「Always Free eligible」の表示が出ることを確認する
2. **Networking**: 新規 VCN を作成（`mytechpulse` コンパートメント内に、パブリックサブネット・パブリック IP 割り当てで）
3. **Add SSH keys**: 手順 2 で作った**公開鍵**（`.pub`）を貼り付ける
4. **Boot volume**: 既定（Always Free のブートボリューム合計 200 GB 以内に収める）
5. Create

### 「Out of Host Capacity」への対処

A1 は人気シェイプなので、作成が何度も失敗するのが普通。エラーは
`Out of host capacity` または `Out of capacity for shape VM.Standard.A1.Flex`。
これは自分の枠の問題ではなく、そのリージョン/可用性ドメインに物理的な空きが無いという意味。

効く順に試す:

1. **可用性ドメイン（AD）を変える** — 複数 AD があるリージョンなら AD-1 → AD-2 → AD-3 と順に試す
2. **要求サイズを小さくする** — 1 OCPU / 6 GB なら通ることがある。**後からスケールアップできる**（インスタンス作成後に Edit → Shape で 2 OCPU / 12 GB に変更）ので、まず小さく確保するのが定石
3. **時間を変えて再試行する** — 空きは解放待ち。数時間〜数日単位で状況が変わる
4. **リトライを自動化する** — 手で押し続けるのは非効率なので OCI CLI で回す

```bash
# ローカル or 別マシンで。事前に oci setup config で認証設定を済ませておく
# --from-json に一度作成に失敗したリクエストのJSONを渡す形が楽（コンソールの
# Create instance 画面の「Save as stack / Show JSON」相当から取得できる）
while ! oci compute instance launch --from-json file://launch-instance.json; do
    echo "$(date +%H:%M:%S) capacity不足。5分後に再試行"
    sleep 300
done
```

粘る前提の作業なので、**この工程は他の作業と並行して回しておく**のがよい。

## 4. Security List / NSG で ingress を開放

VCN → Subnet → Security List（または Network Security Group）の **Ingress Rules** に追加する。

| Source CIDR | プロトコル | 宛先ポート | 用途 |
|---|---|---|---|
| `0.0.0.0/0` | TCP | 22 | SSH（既定で開いていることが多い） |
| `0.0.0.0/0` | TCP | 80 | HTTP（Let's Encrypt の HTTP-01 チャレンジに必須） |
| `0.0.0.0/0` | TCP | 443 | HTTPS（API 本番） |

> **重要**: ここを開けるだけでは 80/443 は通らない。
> Oracle 提供の Ubuntu イメージは OS 側の `iptables` に「以降を全て REJECT する」ルールを持っており、
> Security List を通過したパケットがホストで落とされる。OS 側の対処は次の手順のスクリプトが行う。

## 5. VM の初期セットアップ

SSH でログインする（Ubuntu イメージの既定ユーザーは `ubuntu`）。

```bash
ssh -i ~/.ssh/mytechpulse_oracle ubuntu@<VMのパブリックIP>
```

リポジトリを取得してセットアップスクリプトを実行する。

```bash
git clone https://github.com/H4aruki/MyTechPulse.git
cd MyTechPulse
sudo ./ops/oracle-vm-setup.sh
```

スクリプトがやること（冪等。失敗したら直して再実行してよい）:

1. Docker Engine + Compose plugin の導入（arm64 対応のリポジトリ設定込み）と `ubuntu` ユーザーの docker グループ追加
2. **`iptables` の REJECT ルールより手前に 80/443 の ACCEPT を挿入**し `netfilter-persistent save` で永続化
3. SSH 硬化（パスワード認証・root ログインを無効化）
4. fail2ban の sshd jail 有効化（Ubuntu 24.04 は journald 前提なので `backend = systemd` を明示）
5. タイムゾーンを `Asia/Tokyo` に設定（`ops/backup_db.sh` の日次 cron を意図した時刻で回すため）

実行後、docker グループの反映のために**一度 SSH を切って再ログインする**。

## 6. アプリの起動（#52 の入口）

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

リポジトリルートの `.env` に MySQL の root パスワードを設定する（既定の `rootpass` を本番で使わない）:

```bash
printf 'MYSQL_ROOT_PASSWORD=%s\n' "$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')" > .env
chmod 600 .env
```

起動する（テーブル作成は `backend/entrypoint.sh` の `init_db.py` が自動実行する）:

```bash
docker compose up -d --build
docker compose logs -f api   # 起動ログを確認
```

ARM64 でのイメージビルド確認と全 API の疎通確認は Issue #52 で行う。

## 7. 完了チェックリスト（Issue #50 のクローズ条件）

- [ ] インスタンスと VCN が `mytechpulse` コンパートメント配下にある（root コンパートメントに作っていない）
- [ ] SSH でログインでき、パスワード認証が拒否される（`ssh -o PreferredAuthentications=password ubuntu@<IP>` が失敗する）
- [ ] 再ログイン後、`docker run --rm hello-world` が **sudo なしで**成功する
- [ ] `docker compose version` が Compose v2 を返す
- [ ] ローカル PC から `nc -vz <IP> 22` / `80` / `443` が到達する
      （80/443 はまだ待受プロセスが無いので `Connection refused`。これは**到達している**証拠。
      Security List か iptables が閉じている場合はタイムアウトになる — この違いで切り分ける）
- [ ] `sudo reboot` 後も iptables ルールが残り（`sudo iptables -L INPUT -n`）、Docker が自動起動する
- [ ] `sudo fail2ban-client status sshd` が jail の稼働を返す
- [ ] `timedatectl` が JST を返す

## 次のステップ

- **#51**: ドメイン取得（**課金を伴うためオーナー判断**）と Caddy による HTTPS 化
- **#52**: `docker-compose.yml` の本番起動と ARM64 動作確認
- **#53**: Cloudflare Pages へのフロントエンドデプロイ
- **#54**: GitHub Actions による自動デプロイ
- **#55**: `DEPLOYMENT.md` への統合と VPS 移行ランブック

## 参考

- [Always Free Resources — Oracle Docs](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm)
- [Renaming a Tenancy and Cloud Account — Oracle Docs](https://docs.oracle.com/en-us/iaas/Content/General/Concepts/renamecloudaccount.htm)（*"Free Tier tenancies can't be renamed"*）
- [FAQ on Oracle's Cloud Free Tier](https://www.oracle.com/cloud/free/faq/)（無料アカウントは1人1つまで）
- [Enabling Network Traffic to Ubuntu Images in OCI — Oracle Blogs](https://blogs.oracle.com/developers/enabling-network-traffic-to-ubuntu-images-in-oracle-cloud-infrastructure)（iptables の REJECT ルール問題）
- [Oracle Quietly Halves Free Tier Ampere A1 Compute Limits — InfoQ](https://www.infoq.com/news/2026/07/oracle-cloud-free-tier-limits/)（2026-06-15 の枠縮小）
- [Install Docker Engine on Ubuntu — Docker Docs](https://docs.docker.com/engine/install/ubuntu/)
