# TASKS

MyTechPulseの残タスク一覧。変動が速いため、Obsidian Vaultではなくこのリポジトリ内でGit管理する。随時更新していく生きたドキュメント。

優先度は3分類: **A=デプロイのブロッカー**（本番公開前に必須）/ **B=ユーザー獲得のブロッカー**（一般公開・宣伝開始前に必須）/ **C=後回し可**（機能追加・コード品質）。

最終更新: 2026-08-13

## A. デプロイのブロッカー

構成は**#65 / #67で決着済み**: **フロントエンド（静的LP + React SPA）を Cloudflare Pages、バックエンド + DB を AWS Lightsail 1GB（東京 / $7）に同居**させる2分割構成。合計 月約$8。

- **DBを3つ目に分けない理由**（#65）: テーブル3つ・低頻度書き込みに対しマネージドDBは要件過剰かつ月$8〜25で予算超過。1回の画面表示でDBに複数回問い合わせるため、外部に出すと往復遅延が積み上がる
- **却下した候補**（#67）: Vercel（無料プランが非商用限定）/ GitHub Pages（URL書き換え不可）/ Netlify（転送量上限）/ Xserver VPS（年契約で変更しづらい）/ EC2（通信費が従量）/ GCP無料VM（米国のみ）/ Oracle無料VM（アカウント作成不可）
- **次点**: さくらのVPS（Lightsailが駄目だった場合の最有力）

**#50 → #51 → #52 → #54 → #55 が一本の依存チェーン**になっており、根っこの#50が終わるまで他は着手できない。#53のみ独立して進められるが、`VITE_API_BASE_URL`の確定は#51待ち。

> **Issue #50〜#55 のタイトルはOracle前提のまま残っている。** 内容はLightsailに読み替える（タイトル修正の要否はオーナー判断）。

### #50相当 Lightsailのプロビジョニングと初期セットアップ ← **いま最優先**
- [x] VM初期セットアップスクリプト `ops/oracle-vm-setup.sh` 作成（Docker導入 / SSH硬化 / fail2ban / TZ）。**Lightsailでもそのまま動作する**（arm64チェックは警告のみ、iptables部分はREJECTルールが無ければ末尾追加にフォールバック）
- [x] プロビジョニング手順書 `docs/deploy/lightsail-provisioning.md` 作成（有料プラン切替・静的IP・スワップ2GB）
- [ ] **オーナー作業（課金・登録）**: AWSアカウント作成 → **有料プランへ切り替え**（無料プランのままだと6ヶ月で閉鎖され本番が消える）
- [ ] **オーナー作業**: Lightsailインスタンス作成（東京 `ap-northeast-1a` / Ubuntu 24.04 / $7プラン）＋**静的IPの割り当て**（既定IPは再起動で変わる）
- [ ] **オーナー作業**: IPv4 Firewall で 22 / 80 / 443 開放
- [ ] **オーナー作業**: **スワップ2GB作成**（手順書5節。1GBプランは余裕が薄く、これが無いとバッチやビルドで落ちる）
- [ ] **オーナー作業**: `sudo ./ops/oracle-vm-setup.sh` 実行 → 手順書8節の完了チェックリストを満たす
- [ ] セットアップスクリプトをLightsail向けに整理（`ops/lightsail-vm-setup.sh`へリネーム / スワップ作成の内包 / arm64前提の警告文とiptables節の削除）

### #51 ドメイン取得とCaddyによるHTTPS化（#50依存）
- [ ] **オーナー判断（課金）**: ドメイン取得（Cloudflare Registrarが原価販売）・DNS Aレコードを静的IPへ設定
- [ ] `Caddyfile`新規作成（`api.<domain>` → `api:8000`。Let's Encryptは自動更新）＋`docker-compose.yml`に`caddy`サービス追加
- [ ] APIのドメインもCloudflare経由（プロキシON）にしてサーバーの実IPを隠す

### #52 docker-compose本番起動と疎通確認（#50依存）
- [ ] `postgres:17-alpine` / `python:3.12-slim` のビルド・起動確認、`init_db.py`完了確認、auth/news/click全エンドポイントの疎通
- [ ] `free -h` / `docker stats` でメモリ実測。**スワップを常時数百MB使っていたら$12の2GBプランへ移行を検討**
- [ ] **ARM64動作確認は不要になった**（Lightsail $7プランはx86_64）

### #53 Cloudflare Pagesへのフロントエンドデプロイ（独立して着手可）
- [ ] Pagesプロジェクト作成（root=`frontend/`、build=`npm run build`、output=`dist`）
- [ ] `VITE_API_BASE_URL`に本番APIのURLを設定（#51でドメイン確定後）＋`_redirects`によるSPA直リンク確認
- [ ] 確定したPagesのURLを`CORS_ALLOWED_ORIGINS`に追加（#51とセット）

### #54 GitHub Actionsによる自動デプロイ（#50 / #52依存）
- [ ] **オーナー作業**: デプロイ用SSH鍵生成・Secrets登録
- [ ] mainマージ→LightsailへSSHデプロイするワークフロー追加

### #55 DEPLOYMENT.md整備（他Issue完了ごとに追記）
- [ ] `docs/deploy/lightsail-provisioning.md`を統合＋スケールアップ/移行の判断基準＋さくらVPS移行ランブック＋READMEからのリンク
- [ ] 失効した`docs/deploy/oracle-vm-provisioning.md`を残すか削除するか判断（**削除はオーナー確認事項**）

### 本番運用開始後すぐ
- [ ] 本番用`backend/.env`作成。**SECRET_KEYは本番用に新規生成し開発用と使い回さない**（S12対応）
- [ ] ルート`.env`のDBパスワードを既定値から変更（#63完了後は`POSTGRES_PASSWORD`、未了なら`MYSQL_ROOT_PASSWORD`）
- [ ] `ops/backup_db.sh`のcrontab日次登録
- [ ] **バックアップの外部退避**: 現在は同一ホストの`backups/`に保存しており、インスタンス全損で失われる。Cloudflare R2（10GB無料枠）等へ逃がす

## B. ユーザー獲得のブロッカー（一般公開・宣伝開始前に必須）

- [ ] **利用規約・プライバシーポリシーページ＋サインアップ同意チェックボックス**: 個人情報（ユーザー名・パスワード）を収集するのに同意導線が皆無。法的リスクが最も明確な項目。`frontend/src/pages/`に新規ページ＋`SignupPage.tsx`に同意チェック＋バックエンド側の未同意拒否
- [ ] **レート制限・ブルートフォース対策**: `login_check`に試行回数制限が一切ない。slowapi等でIPベースの簡易レート制限を導入
- [ ] **S10: パスワード/ユーザー名の長さ・複雑性検証**: レート制限とセットで「弱いパスワード×無制限試行」の最悪の組み合わせを解消
- [ ] **ロギング基盤・本番監視**: エラーハンドリングが全て`print(f"Error: {e}")`形式（auth_service.py / click_service.py / news_service.py）。標準loggingへの置き換え＋Sentry等の導入で実ユーザーの障害を検知可能にする
- [ ] **DBバックアップの実運用開始**: スクリプトはPR#42で用意。実ユーザーのデータが乗った時点から必須（crontab登録はAの外部作業）

## C. 後回し可（機能追加・コード品質）

### オープンなGitHub Issue
- [ ] **#66**: 記事取得を「画面を開くたび」から日次の一括取得＋DB保存へ変更。**タグ100個を1日1回舐めても約200リクエスト/日で済み、この回数はユーザー数に依存しなくなる**（現状は利用者数×リロード数で増えQiitaの上限1000req/hに達する）。外部APIダウン時も記事を出せるようになる副次効果あり。要検討: 古い記事の保持期間、実行時刻と1日の回数
- [ ] **#63**: MySQL 8.4 → PostgreSQL 17 移行（**A章のデプロイと並行して先行着手可**。`feat/db-postgres-migration`ブランチは未着手）
- [ ] **#33**: 外部API（Qiita/Zenn/将来のX等）の部分的失敗が記事の多様性を静かに損なう問題（情報欠落の可視化・リトライ戦略）
- [ ] **#22**: Qiita記事が複数タグにマッチしてもタグがマージされずスコアが過小評価される
- [ ] **#14**: APIレスポンスをHTTPステータスコード方式へ全面移行（設計方針は Obsidian `notes/2026-07-09_...` に整理済み）
- [ ] **#47**: サインアップのタグリスト（`frontend/src/constants/tags.ts`）をバックエンドから動的取得にするか検討
- [ ] **#46**: JWTの保管をlocalStorageからhttpOnly Cookieへ移行（実施する場合は`allow_credentials`とCORS設定も戻す必要あり）
- [ ] **#45**: 未使用の可能性があるCORS許可オリジン（localhost:3000 / 127.0.0.1:8000）の調査

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
- [ ] フェーズ1（DB記事キャッシュ・CI/CD整備）への移行判断。**DB記事キャッシュは#66として起票済み**、CI/CDは#54でAに含まれる

## 直近完了（記録）

- V4 ユーザー列挙対策（Issue#28 → PR#29）: mainマージ済み
- V5前半 init_db.pyのDB名SQL文f-string埋め込み対策（PR#31）: mainマージ済み
- Issue#23 Qiita/ZennのHTTPStatusError未処理修正（PR#32）: mainマージ済み
- ローカル環境整理（main最新化・マージ済みブランチ`fix/init-db-sql-injection-guard`/`fix/news-service-http-status-error`削除）: 2026-07-16完了
- 2026-07-16 デプロイ準備Issue一括起票: #34（技術スタック議論）/ #35 / #37 / #39 / #41 / #43
- **旧A章の実装タスク4件がmainマージ済み**: #35→PR#36（CORS環境変数化・`allow_credentials`削除）/ #37→PR#38（API_BASE_URL自動切替）/ #39→PR#40（QiitaタグURLエンコード）/ #41→PR#42（`ops/backup_db.sh`追加）
- **フロントエンドのVite + React + TypeScript化（PR#49）: mainマージ済み**（旧`html/`・`js/`は削除済み）
- **#34 技術スタック選定に決着**（2026-07-28）: Oracle Free VM + Cloudflare Pages + Caddy + GitHub Actions。子Issue #50〜#55 を起票
- **デプロイ先の再選定に決着**（2026-08-13、#64 → #65 / #66 / #67）: Oracleはアカウント作成不可で断念、#59のGCP e2-micro + SQLite案も前提失効。**2分割（Cloudflare Pages + AWS Lightsail 1GB東京）に確定**し、DBはPostgreSQLでバックエンドに同居。記事の一括取得は#66へ切り出し
