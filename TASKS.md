# TASKS

MyTechPulseの残タスク一覧。変動が速いため、Obsidian Vaultではなくこのリポジトリ内でGit管理する。随時更新していく生きたドキュメント。

優先度は3分類: **A=デプロイのブロッカー**（本番公開前に必須）/ **B=ユーザー獲得のブロッカー**（一般公開・宣伝開始前に必須）/ **C=後回し可**（機能追加・コード品質）。

最終更新: 2026-07-30

## A. デプロイのブロッカー

構成は**#34で決着済み**: バックエンド（FastAPI + MySQL）を Oracle Cloud Always Free VM（Ampere A1 / ARM）で docker compose 運用、フロントエンド（静的LP + React SPA）を Cloudflare Pages。Render / Cloud Run / TiDB は検討の上で却下。

**#50 → #51 → #52 → #54 → #55 が一本の依存チェーン**になっており、根っこの#50が終わるまで他は着手できない。#53のみ独立して進められるが、`VITE_API_BASE_URL`の確定は#51待ち。

### #50 Oracle VMのプロビジョニングと初期セットアップ ← **いま最優先**
- [x] VM初期セットアップスクリプト `ops/oracle-vm-setup.sh` 作成（Docker導入 / iptablesのREJECT問題対処 / SSH硬化 / fail2ban / TZ）
- [x] プロビジョニング手順書 `docs/deploy/oracle-vm-provisioning.md` 作成（リージョン選定・容量エラー時のリトライ手順）
- [ ] **オーナー作業**: Oracleアカウント作成。ホームリージョン=`ap-tokyo-1`狙い、アカウント名は`haruki-cloud`等の中立な名前（**どちらも後から変更不可**。プロダクトの分離はコンパートメントで行う）
  - ⚠️ **2026-07-30時点ブロック中**: サインアップが原因非開示の汎用エラーで完了できない。切り分けと問い合わせ導線は手順書1-5節を参照。Free TierはSR不可のためCloud Support Chatが唯一の窓口
  - 長期化する場合は**フェーズAを飛ばして国内VPS（#55のフェーズB）へ切り替える**判断がある（課金判断。x86なら#52のARM64確認が不要になる）
- [ ] **オーナー作業**: A1インスタンス作成（「Out of Host Capacity」対策は手順書4節を参照）
- [ ] **オーナー作業**: Security List / NSG で ingress 22 / 80 / 443 開放
- [ ] **オーナー作業**: `sudo ./ops/oracle-vm-setup.sh` 実行 → 手順書7節の完了チェックリストを満たす

### #51 ドメイン取得とCaddyによるHTTPS化（#50依存）
- [ ] **オーナー判断（課金）**: ドメイン取得・DNS Aレコード設定
- [ ] `Caddyfile`新規作成（`api.<domain>` → `api:8000`。Let's Encryptは自動更新）＋`docker-compose.yml`に`caddy`サービス追加

### #52 docker-compose本番起動とARM64動作確認（#50依存）
- [ ] `mysql:8.4` / `python:3.12-slim`（arm64）のビルド・起動確認、`init_db.py`完了確認、auth/news/click全エンドポイントの疎通

### #53 Cloudflare Pagesへのフロントエンドデプロイ（独立して着手可）
- [ ] Pagesプロジェクト作成（root=`frontend/`、build=`npm run build`、output=`dist`）
- [ ] `VITE_API_BASE_URL`に本番APIのURLを設定（#51でドメイン確定後）＋`_redirects`によるSPA直リンク確認
- [ ] 確定したPagesのURLを`CORS_ALLOWED_ORIGINS`に追加（#51とセット）

### #54 GitHub Actionsによる自動デプロイ（#50 / #52依存）
- [ ] **オーナー作業**: デプロイ用SSH鍵生成・Secrets登録
- [ ] mainマージ→Oracle VMへSSHデプロイするワークフロー追加

### #55 DEPLOYMENT.md整備（他Issue完了ごとに追記）
- [ ] `docs/deploy/oracle-vm-provisioning.md`を統合＋無料枠限界の判断基準＋さくらVPS移行ランブック＋READMEからのリンク

### 本番運用開始後すぐ
- [ ] 本番用`backend/.env`作成。**SECRET_KEYは本番用に新規生成し開発用と使い回さない**（S12対応）
- [ ] ルート`.env`の`MYSQL_ROOT_PASSWORD`を既定値`rootpass`から変更
- [ ] `ops/backup_db.sh`のcrontab日次登録（可能ならrclone等で外部ストレージ退避も）

## B. ユーザー獲得のブロッカー（一般公開・宣伝開始前に必須）

- [ ] **利用規約・プライバシーポリシーページ＋サインアップ同意チェックボックス**: 個人情報（ユーザー名・パスワード）を収集するのに同意導線が皆無。法的リスクが最も明確な項目。`frontend/src/pages/`に新規ページ＋`SignupPage.tsx`に同意チェック＋バックエンド側の未同意拒否
- [ ] **レート制限・ブルートフォース対策**: `login_check`に試行回数制限が一切ない。slowapi等でIPベースの簡易レート制限を導入
- [ ] **S10: パスワード/ユーザー名の長さ・複雑性検証**: レート制限とセットで「弱いパスワード×無制限試行」の最悪の組み合わせを解消
- [ ] **ロギング基盤・本番監視**: エラーハンドリングが全て`print(f"Error: {e}")`形式（auth_service.py / click_service.py / news_service.py）。標準loggingへの置き換え＋Sentry等の導入で実ユーザーの障害を検知可能にする
- [ ] **DBバックアップの実運用開始**: スクリプトはPR#42で用意。実ユーザーのデータが乗った時点から必須（crontab登録はAの外部作業）

## C. 後回し可（機能追加・コード品質）

### オープンなGitHub Issue
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
- [ ] フェーズ1（DB記事キャッシュ・CI/CD整備）への移行判断（CI/CDは今回のAで一部前進）

## 直近完了（記録）

- V4 ユーザー列挙対策（Issue#28 → PR#29）: mainマージ済み
- V5前半 init_db.pyのDB名SQL文f-string埋め込み対策（PR#31）: mainマージ済み
- Issue#23 Qiita/ZennのHTTPStatusError未処理修正（PR#32）: mainマージ済み
- ローカル環境整理（main最新化・マージ済みブランチ`fix/init-db-sql-injection-guard`/`fix/news-service-http-status-error`削除）: 2026-07-16完了
- 2026-07-16 デプロイ準備Issue一括起票: #34（技術スタック議論）/ #35 / #37 / #39 / #41 / #43
- **旧A章の実装タスク4件がmainマージ済み**: #35→PR#36（CORS環境変数化・`allow_credentials`削除）/ #37→PR#38（API_BASE_URL自動切替）/ #39→PR#40（QiitaタグURLエンコード）/ #41→PR#42（`ops/backup_db.sh`追加）
- **フロントエンドのVite + React + TypeScript化（PR#49）: mainマージ済み**（旧`html/`・`js/`は削除済み）
- **#34 技術スタック選定に決着**（2026-07-28）: Oracle Free VM + Cloudflare Pages + Caddy + GitHub Actions。子Issue #50〜#55 を起票
