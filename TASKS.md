# TASKS

MyTechPulseの残タスク一覧。変動が速いため、Obsidian Vaultではなくこのリポジトリ内でGit管理する。随時更新していく生きたドキュメント。

優先度は3分類: **A=デプロイのブロッカー**（本番公開前に必須）/ **B=ユーザー獲得のブロッカー**（一般公開・宣伝開始前に必須）/ **C=後回し可**（機能追加・コード品質）。

最終更新: 2026-08-31

## A. デプロイのブロッカー

構成は**#65 / #67で決着済み**: **フロントエンド（静的LP + React SPA）を Cloudflare Pages、バックエンド + DB を AWS Lightsail 1GB（東京 / $7）に同居**させる2分割構成。合計 月約$8。

- **DBを3つ目に分けない理由**（#65）: テーブル3つ・低頻度書き込みに対しマネージドDBは要件過剰かつ月$8〜25で予算超過。1回の画面表示でDBに複数回問い合わせるため、外部に出すと往復遅延が積み上がる
- **却下した候補**（#67）: Vercel（無料プランが非商用限定）/ GitHub Pages（URL書き換え不可）/ Netlify（転送量上限）/ Xserver VPS（年契約で変更しづらい）/ EC2（通信費が従量）/ GCP無料VM（米国のみ）/ Oracle無料VM（アカウント作成不可）
- **次点**: さくらのVPS（Lightsailが駄目だった場合の最有力）

**本番稼働中（2026-08-17〜）**: フロント **https://mytechpulse.net** / API **https://api.mytechpulse.net**

**プロビジョニング → HTTPS化 → 本番起動 → 自動デプロイ → 手順書 が一本の依存チェーン**だったが、**自動デプロイと手順書を除く工程はすべて完了**（Cloudflare Pagesへのデプロイ含む）。残るA章のタスクは **#54（自動デプロイ）** と **#55（DEPLOYMENT.md作成）**、および運用まわりの細部のみ。デプロイのブロッカーとしては解消済みで、次に効くのは**B章（一般公開前に必須の項目）**。

> **2026-08-18にIssueを棚卸しした（#77）。** Oracle前提で失効していたIssueはクローズするか現行構成に書き直してある。以下の見出しのうち、完了してIssueを閉じたものは「(closed)」、GitHubからIssueごと削除されたものは「(Issue削除済み)」と表記する。

### Lightsailのプロビジョニングと初期セットアップ ✅ **完了（2026-08-14）**（#50 closed）
本番インスタンス: 東京 `ap-northeast-1a` / Ubuntu 24.04.4 LTS / x86_64 / 静的IP `54.168.29.67`

- [x] VM初期セットアップスクリプト `ops/oracle-vm-setup.sh` 作成（Docker導入 / SSH硬化 / fail2ban / TZ）。**Lightsailでもそのまま動作する**（arm64チェックは警告のみ、iptables部分はREJECTルールが無ければ末尾追加にフォールバック）
- [x] プロビジョニング手順書 `docs/deploy/lightsail-provisioning.md` 作成（有料プラン切替・静的IP・スワップ2GB）
- [x] **オーナー作業（登録）**: AWSアカウント作成（2026-08-13完了。MFA / 請求アラート / リージョン東京 / カード登録まで済）
- [x] **オーナー作業（課金）**: 有料プランへ切り替え（2026-08-14完了）
- [x] **オーナー作業**: Lightsailインスタンス作成（$7プラン / Dual-stack）＋静的IPの割り当て
- [x] **オーナー作業**: IPv4 Firewall で 22 / 80 / 443 開放（80/443はAnywhere IPv4+IPv6）
- [x] **スワップ2GB作成**（`vm.swappiness=10`。`/etc/fstab`に登録済み）
- [x] `sudo bash ops/oracle-vm-setup.sh` 実行 → 手順書8節のチェックリストを全て満たすことを確認（sudoなしdocker / Compose v5.4.0 / JST / パスワード認証・rootログイン無効 / fail2ban稼働）
- [x] **再起動テスト実施（2026-08-17）**。`sudo systemctl reboot` 後、**約40秒でAPIが自動復帰**。スワップ2GB / `vm.swappiness=10` / iptablesの80・443 ACCEPT / crontab / JST / fail2ban / 静的IP のすべてが維持され、3コンテナとも `restart: unless-stopped` で自動起動した
- [ ] セットアップスクリプトをLightsail向けに整理（`ops/lightsail-vm-setup.sh`へリネーム / スワップ作成の内包 / arm64前提の警告文とiptables節の削除）。**完了メッセージが`MYSQL_ROOT_PASSWORD`とOracleの手順書を案内したままなので併せて直す**（#63でDBがPostgreSQLに変わったため誤った案内になっている）。**#50をクローズしたので、この項目がこの作業の唯一の記録になる**

### ドメイン取得とCaddyによるHTTPS化 ✅ **完了（2026-08-17）**（#51 closed）
本番URL: フロント **https://mytechpulse.net** / API **https://api.mytechpulse.net**

- [x] **オーナー判断（課金）**: `mytechpulse.net` を Cloudflare Registrar で取得（`.com` は取得済みだったため `.net`）。`api` のAレコードを静的IP `54.168.29.67` へ設定
- [x] `Caddyfile`新規作成＋`docker-compose.yml`に`caddy`サービス追加（PR#72）。Let's Encrypt証明書の取得を確認（有効期限 2026-11-12）
- [x] APIのドメインもCloudflare経由（プロキシON）にした。SSL/TLSモードは**フル（厳格）**。DNSからは実IPが消え、エッジは東京（`CF-RAY: ...-NRT`）、`cf-cache-status: DYNAMIC` でAPIレスポンスはキャッシュされていない
- [ ] **証明書の自動更新の確認（2026-10月頃）**。プロキシONにしたことでTLS-ALPN-01が使えなくなりHTTP-01へフォールバックする。Caddyが自動で切り替えるが、**初回の自動更新が通ることを一度確認する必要がある**
- [ ] オリジンへの直接アクセスを塞ぐ → **#74**（実IPを知られていると Host ヘッダー付きで直接叩ける。優先度は低く、実ユーザーを迎える前に着手）

### docker-compose本番起動と疎通確認 ✅ **完了（2026-08-14）**（Issue削除済み）
- [x] `postgres:17-alpine` / `python:3.12-slim` のビルド・起動確認、`init_db.py`完了確認、auth/news/click全エンドポイントの疎通
      （サインアップ→ログイン→誤パスワードで`status:2`→記事取得Qiita10件/Zenn10件→クリック学習→トークン無しで401→CASCADE削除まで本番で確認）
- [x] `free -h` / `docker stats` でメモリ実測。**結果: 起動直後 523Mi/911Mi 使用・スワップ59Mi・コンテナ合計121MiB（api 64 + db 57）。
      見込みの580MiBを下回り余裕がある。$12の2GBプランへの移行は当面不要**
- [x] **ARM64動作確認は不要になった**（Lightsail $7プランはx86_64）
- [ ] 記事の日次バッチ（#66）を動かした後にメモリを再測する。ピークはそこで出る

### Cloudflare Pagesへのフロントエンドデプロイ ✅ **完了（2026-08-17）**（#53 closed）
- [x] Pagesプロジェクト作成（2026-08-13。root=`frontend/`、build=`npm run build`、output=`dist`、フレームワークプリセット=なし）。**Workersの作成フロー（`npx wrangler deploy`）ではなくPagesを選ぶこと** — 静的成果物を配るだけで`frontend/public/_redirects`をそのまま解釈できる
  - **旧記録の「非本番ブランチのビルド=オフ」は誤りだった**（#95で判明）。実際には作業用の枝でもCloudflareがビルドし、下見用URLを作っていた。**2026-08-31にGit連携ごと解除したため、この設定は存在しない**
- [x] 初回デプロイ成功。公開URL: **https://mytechpulse.pages.dev/**
- [x] `VITE_API_BASE_URL`に `https://api.mytechpulse.net` を設定（Pagesの環境変数・テキスト型。**ビルド時に埋め込まれるので変更後は再デプロイが必須**）
  - **2026-08-31以降、この値は使われない**。組み立てがGitHub Actions側へ移ったため、`ci.yml`の公開ジョブに書いた値が効く（#95）。Cloudflare側に残っている設定は無害だが紛らわしいので、いずれ消す
- [x] `mytechpulse.net` / `www.mytechpulse.net` をカスタムドメインとして追加
- [x] `_redirects`によるSPA直リンクを修正（PR#73）。**Pagesは書き換え先から`.html`と`/index`を剥がすため`/app/index.html`はループ判定でルールごと無視される**。SPAエントリを`app.html`へ移し`/app/*  /app  200`とした
- [x] `CORS_ALLOWED_ORIGINS`に `https://mytechpulse.net,https://www.mytechpulse.net,https://mytechpulse.pages.dev` を設定

### #91 プルリクエストのたびに書き方チェックを自動で走らせる ✅ **完了（2026-08-30）**（PR#92）
- [x] `.github/workflows/ci.yml`追加（サーバー側=Ruff、画面側=oxlint＋型チェック）
- [x] `main`のRulesetにチェック通過の必須条件を追加（2026-08-30）。**古い方のBranch protectionではなくRulesetsを使っている**
- [ ] 見た目のズレ（27ファイル）を一括で整えてから、整形チェックも必須に引き上げる
- テストの自動実行とカバレッジは、テストコードを書く段階で別Issueにする（現時点でテストが0件のため）

### #93 main以外の枝でも自動チェックを走らせる ✅ **完了（2026-08-30）**（PR#94）
- [x] `ci.yml`のきっかけから枝の絞り込みを外す（どの枝へのpush・どの枝あてのPRでも実行）
- [x] PR側のきっかけは「作られたとき」だけに限定。**更新分はpushが拾い、その結果がそのまま必須条件として扱われる**ので二重実行を避けられる
- [x] `CONTRIBUTING.md`の保護設定手順をRulesets版に修正

### mainの保護設定（Ruleset）に入れたもの・保留にしたもの
- [x] チェック通過を必須に（2026-08-30）
- [x] 取り込み方をSquashのみに制限（2026-08-30）。**1機能=1コミットを厳守するため**。PR#92は通常のマージで入ってしまっていた
- [ ] **Require branches to be up to date を追加する（保留）**
  - **根拠**: 別々のPRがそれぞれ単独では緑でも、順に取り込むと壊れる事故を防げる（片方が「古いmain」を前提に緑になっているため）
  - **いま入れない理由**: mainが動くたびに開いている全PRで更新ボタンを押して再確認を待つ必要が出る。同時PRが1〜2本の現状では手間が上回る
  - **入れどきの目安**: 同時に開くPRが3本以上常態化したとき、または複数人開発になったとき
  - 詳細は`CONTRIBUTING.md`の4章に記載

### #95 CIが緑のときだけCloudflare Pagesにビルドさせる ✅ **完了（2026-08-31）**（PR#96）
公開の主導権をCloudflare側からGitHub Actions側へ移した。書き方チェックの2つが両方とも緑のときだけ公開の工程が動く。

- [x] **オーナー作業**: Cloudflare APIトークンを発行し、GitHubのSecretsに登録（2026-08-30）
  - 権限は **Account → Cloudflare Pages → Edit** の1行のみ。対象アカウントも1つに絞ってある
  - Secrets名は `CLOUDFLARE_API_TOKEN` / `CLOUDFLARE_ACCOUNT_ID`。`ci.yml`がこの名前で読む
  - **クライアントIPフィルタは空にすること**。実行元はGitHub側の使い捨てマシンで毎回IPが変わる
- [x] 公開の工程を`ci.yml`に追加（`needs`で書き方チェック2つを条件にし、`main`へのpushだけに限定）
- [x] `main`での初回実行が成功（2026-08-31。11ファイル＋`_redirects`を配信、所要13秒）
- [x] **オーナー作業**: Cloudflare側のGit連携を解除（2026-08-31）

**運用上おぼえておくこと**:
- 接続先APIのURLは**組み立て時に中へ埋め込まれる**。変更するときは`ci.yml`の公開ジョブを直す（Cloudflare側の環境変数はもう効かない）
- `--project-name=mytechpulse` / `--branch=main` を前提にしている。Pages側のプロジェクト名か本番ブランチ名を変えるなら`ci.yml`も直す
- `main`の実行は後続のpushがあっても打ち切らない設定にしてある（配信の途中で止まらないようにするため）
- **トークンに有効期限を付けた場合、切れた日から公開だけが静かに止まる**。今回は無期限で発行している
- 発行したトークンは**本人に紐づく**もの。共同開発になったら、契約に紐づくトークン（Manage Account → Account API Tokens）へ作り直してSecretsの値を差し替える。名前は同じなので`ci.yml`は触らなくてよい

### #54 mainマージ時にLightsailへ自動デプロイする（依存する前工程はすべて完了済み）
- [ ] **オーナー作業**: デプロイ用SSH鍵生成・Secrets登録
- [ ] mainマージ→LightsailへSSHデプロイするワークフロー追加

### #55 DEPLOYMENT.mdを作成する（Lightsail運用手順＋移行ランブック）
- [ ] `docs/deploy/lightsail-provisioning.md`を統合＋スケールアップ/移行の判断基準＋さくらVPS移行ランブック＋READMEからのリンク
- [ ] 失効した`docs/deploy/oracle-vm-provisioning.md`を残すか削除するか判断（**削除はオーナー確認事項**）

### 本番運用開始後すぐ
- [x] 本番用`backend/.env`作成（2026-08-14）。**SECRET_KEYはサーバー上で新規生成**し開発用とは別の値（S12対応）。`CORS_ALLOWED_ORIGINS`にPagesのURLを設定済み。パーミッションは600
- [x] ルート`.env`の`POSTGRES_PASSWORD`をランダム値（32文字）に設定。パーミッションは600
- [x] `ops/backup_db.sh`のcrontab日次登録（2026-08-17）。`0 4 * * *`（サーバーはJSTなので毎日午前4時）。ログは`backups/backup.log`。**実行権限が抜けていたため`fix/backup-script-exec-bit`（PR#75）で修正**
- [ ] **バックアップの外部退避**: 現在は同一ホストの`backups/`に保存しており、インスタンス全損で失われる。Cloudflare R2（10GB無料枠）等へ逃がす

## B. ユーザー獲得のブロッカー（一般公開・宣伝開始前に必須）

- [ ] **利用規約・プライバシーポリシーページ＋サインアップ同意チェックボックス**: 個人情報（ユーザー名・パスワード）を収集するのに同意導線が皆無。法的リスクが最も明確な項目。`frontend/src/pages/`に新規ページ＋`SignupPage.tsx`に同意チェック＋バックエンド側の未同意拒否
- [ ] **レート制限・ブルートフォース対策**: `login_check`に試行回数制限が一切ない。slowapi等でIPベースの簡易レート制限を導入
- [ ] **S10: パスワード/ユーザー名の長さ・複雑性検証**: レート制限とセットで「弱いパスワード×無制限試行」の最悪の組み合わせを解消
- [ ] **ロギング基盤・本番監視**: エラーハンドリングが全て`print(f"Error: {e}")`形式（auth_service.py / click_service.py / news_service.py）。標準loggingへの置き換え＋Sentry等の導入で実ユーザーの障害を検知可能にする
- [x] **DBバックアップの実運用開始**: crontab登録済み（2026-08-17）。残るのは**外部退避**（A章）で、現状はインスタンス全損でバックアップごと消える

## C. 後回し可（機能追加・コード品質）

### オープンなGitHub Issue
- [ ] **#66**: 記事取得を「画面を開くたび」から日次の一括取得＋DB保存へ変更。**タグ100個を1日1回舐めても約200リクエスト/日で済み、この回数はユーザー数に依存しなくなる**（現状は利用者数×リロード数で増えQiitaの上限1000req/hに達する）。外部APIダウン時も記事を出せるようになる副次効果あり。要検討: 古い記事の保持期間、実行時刻と1日の回数
- [ ] **#74**: オリジンへの直接アクセスを塞いでCloudflare経由に限定する（80/443をCloudflareのIPレンジからのみ許可）。**実ユーザーを迎える前に着手**
- [ ] **#33**: 外部API（Qiita/Zenn/将来のX等）の部分的失敗で記事の多様性が静かに損なわれる問題の対策を決める（情報欠落の可視化・リトライ戦略）
- [ ] **#22**: Qiita記事が複数タグにマッチしてもタグがマージされずスコアが過小評価される
- [ ] **#14**: APIレスポンスをHTTPステータスコード方式へ全面移行（設計方針は Obsidian `notes/2026-07-09_...` に整理済み）
- [ ] **#47**: サインアップのタグリスト（`frontend/src/constants/tags.ts`）をバックエンドから動的取得にするか検討
- [ ] **#46**: JWTの保管をlocalStorageからhttpOnly Cookieへ移行（実施する場合は`allow_credentials`とCORS設定も戻す必要あり）
- [ ] **#45**: 開発用のCORS許可オリジンを棚卸しする。3000 / 8000 に加え、**Live Server用の5500系2件もReact化（PR#49）で不要になった可能性**がある

### コード品質（要再確認・未再検証）
- [ ] CR#3: 外部APIフィールドのハード添字参照によるKeyError/ValueError
- [ ] CR#5: クリック学習とスコアリングでタグ名の大文字小文字が不一致
- [ ] CR#10: Zenn→Article変換・get-or-createタグロジックのコード重複
- [ ] CR#11: タグ取得のN+1クエリ（`joinedload`未使用）

### 基盤整備
- [ ] **Alembic導入（DBマイグレーション管理）**: 現状は`init_db.py`の`create_all`のみでスキーマ変更に対応不可。**次にスキーマ変更を伴う機能（ブックマーク・既読管理等）に着手する前に導入必須**
- [ ] pytest導入・テストカバレッジ確保（現状0件。優先はauth周りから）
- [ ] パスワードリセット機能基盤（メール送信・現状メールアドレス自体を持たない設計のためスキーマ変更込み）

### README Roadmap（未実装機能）
- [ ] X記事の追加
- [ ] キーワード検索
- [ ] ダークモード対応
- [ ] ブックマーク
- [ ] 既読管理
- [ ] AI要約機能
- [ ] 自動カテゴリ分類

### フェーズ判断
- [ ] Gitブランチルール運用の定着（実践フェーズ）
- [ ] フェーズ1（DB記事キャッシュ・CI/CD整備）への移行判断。**DB記事キャッシュは#66として起票済み**、CI/CDは#54（自動デプロイ）と#91（書き方チェック）でAに含まれる

## 直近完了（記録）

- V4 ユーザー列挙対策（Issue#28 → PR#29）: mainマージ済み
- V5前半 init_db.pyのDB名SQL文f-string埋め込み対策（PR#31）: mainマージ済み
- Issue#23 Qiita/ZennのHTTPStatusError未処理修正（PR#32）: mainマージ済み
- ローカル環境整理（main最新化・マージ済みブランチ`fix/init-db-sql-injection-guard`/`fix/news-service-http-status-error`削除）: 2026-07-16完了
- 2026-07-16 デプロイ準備Issue一括起票: #34（技術スタック議論）/ #35 / #37 / #39 / #41 / #43
- **旧A章の実装タスク4件がmainマージ済み**: #35→PR#36（CORS環境変数化・`allow_credentials`削除）/ #37→PR#38（API_BASE_URL自動切替）/ #39→PR#40（QiitaタグURLエンコード）/ #41→PR#42（`ops/backup_db.sh`追加）
- **フロントエンドのVite + React + TypeScript化（PR#49）: mainマージ済み**（旧`html/`・`js/`は削除済み）
- **#34 技術スタック選定に決着**（2026-07-28）: Oracle Free VM + Cloudflare Pages + Caddy + GitHub Actions。子Issue #50〜#55 を起票。**ただしこの結論は後に #67 で上書きされた**（推奨案だったさくらVPS 2GB + パスベース同一オリジン + MySQL はいずれも不採用。#34 は2026-08-18にクローズ済み）
- **#63 MySQL 8.4 → PostgreSQL 17 移行（PR#70）: mainマージ済み**（2026-08-14）。ローカル開発DBもDockerコンテナに一本化しXAMPPは不要になった
- **デプロイ先の再選定に決着**（2026-08-13、#64 → #65 / #66 / #67）: Oracleはアカウント作成不可で断念、GCP e2-micro + SQLite案（Issue削除済み）も前提失効。**2分割（Cloudflare Pages + AWS Lightsail 1GB東京）に確定**し、DBはPostgreSQLでバックエンドに同居。記事の一括取得は#66へ切り出し
- **Issue/PRの棚卸し**（2026-08-18、#77）: 失効した前提のIssueを整理。**クローズ** = #34（結論が#67に上書き）/ #50（Oracle断念・Lightsailで完了）/ #51 / #53（いずれも完了）/ #57・PR#58（LPリデザインは起票し直し）。**現行構成に改訂** = #14 / #33 / #45 / #46 / #47 / #54 / #55 / #60。オーナー手書きの #64 / #65 / #66 / #67 は編集していない
