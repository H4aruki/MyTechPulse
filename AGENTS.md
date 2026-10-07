# MyTechPulse エージェント作業ルール

このファイルは、Codex と Claude Code が共通で参照するプロジェクト固有ルールの正本です。
共通ルールを変更するときは、このファイルを先に更新してください。

## プロジェクト概要

MyTechPulse は、Qiita と Zenn から利用者の興味タグに合う記事を集め、優先順位を付けて届ける技術ニュースアプリです。

- フロントエンド: Vite、React、TypeScript。`frontend/`
- バックエンド: Go、PostgreSQL 17。`server/`
- 公開: `main` への反映をきっかけに、検査が全部成功したものだけを、`.github/workflows/ci.yml` が本番へ自動で入れ替える（Go版API・画面）。リポジトリ変数 `GO_DEPLOY_ENABLED` が `true` のときだけ動く。詳しくは `docs/deploy/go-auto-deploy.md`

## 作業開始時

1. このファイルと、対象ディレクトリにある追加の `AGENTS.md` を読む。
2. 対象作業に合う `.agents/skills/` または `.claude/skills/` の Skill を読む。
3. `git status --short --branch` で現在地と未コミット変更を確認する。
4. 新しい作業は、着手前に目的と理由を書いたGitHub Issueを作成する。対応する既存Issueがあれば新規作成しない。
5. 新しい変更を始める場合は、`main` で `git pull --ff-only` を実行してから `<type>/<kebab-case>` の作業ブランチを作る。
6. 既存の未コミット変更は利用者のものとして扱い、勝手に戻したり上書きしたりしない。

## 禁止事項と許可が必要な操作

- `.env`、`server/.env`、`frontend/.env` を読まない。項目名は `.env.example`、`server/.env.example`、`frontend/.env.example` で確認する。
- `git push --force`、`git reset --hard`、`git clean` など、履歴や作業内容を失う操作をしない。
- `main` へ直接 push しない。
- PR の承認とマージをしない。最終判断は人間が行う。
- DBボリュームを削除しない。停止だけなら `docker compose down` を使う。
- 本番サーバーへ直接接続せず、手動公開もしない。
- リポジトリ変数 `GO_DEPLOY_ENABLED`（自動デプロイのスイッチ）を、作成・変更・削除しない。オーナーが行う。
- 配布（`release.yml`）の手動実行は、箱を公開の置き場へ送る外部への送信にあたるので、明示的な許可を得てから行う。
- DBの変更（`server/db/migrations/`）は、追加だけにする（削除・名前の変更・型の変更をしない）。自動デプロイが入れ替えに失敗して直前の版へ戻ったとき、DBは巻き戻らないため。`additive_test.go` が確かめる。
- 既存ファイルや既存データを削除する前に、対象・役割・理由・影響を説明して許可を得る。
- ライブラリやフレームワークを追加する前に、名称・理由・影響を説明して許可を得る。
- GitHub操作以外の外部公開・送信は、内容を準備してから明示的な許可を得る。
- 課金やサブスクリプション登録をしない。

機械的な防御は `.claude/settings.json`、`.claude/hooks/`、`.codex/` にあります。文章と防御設定は同じ方針を保ってください。

## 双方向サブエージェント

- 通常の依頼では、現在のエージェントだけで作業する。
- `/multi`、`/parallel`、または明確な並列委譲の依頼がある場合だけ、親は共通ランナーを使える。
- workerは別エージェントを起動せず、専用worktree内で担当範囲だけを変更・検証・通常コミットし、構造化結果を親へ返す。
- 親は担当重複を避け、結果と差分をレビューする。worker結果の自動統合、マージ、worktree削除は行わない。
- 同じツールを内部のサブエージェントとして呼ぶ場合は、Claude Codeは `sonnet` / `high`、Codexは `luna` / `high` を既定にする。上限として自動で下げる仕組みはなく、指定した段階で固定して呼ぶ。設定は `.claude/settings.json`、`.claude/agents/`、`.codex/config.toml`。

## 変更前と完了時の報告

コードや設定を変更する前に、次を日本語で短く報告します。

1. 原因: 問題を起こしているもの。未確定なら推測と明記する。
2. 修正方法: どこをどう変えるか。
3. ゴール: 変更後に何ができる、または防げるか。

完了時は、変更内容、実行した検証と結果、未解決事項を報告します。未実施の検証は理由も書きます。

## 実装方針

- 依頼範囲を不必要に広げず、既存の構造と命名を優先する。
- 1機能を1コミットにまとめる。コミット規約は `CONTRIBUTING.md` に従う。
- API、認証、DB、画面の境界を変更するときは、関連する両側を同時に確認する。
- 自動生成物や一時ファイルを成果物へ混ぜない。
- 事実、推測、未確認事項を区別する。

## 開発コマンド

Windowsでは、次の1コマンドで、DB・DBの移行・Go API・フロントエンドを起動できます（`server\.env` と `frontend\.env` が必要です）。

```powershell
.\dev.ps1
```

手順を分けて実行する場合は、PostgreSQLを先に起動します。

```bash
docker compose up -d db
```

Go APIは `server/` から実行します。設定は環境変数で渡します（`server/.env.example` の項目を参照）。

```bash
cd server
go run ./cmd/migrate   # DBの移行（追加だけ）。APIの起動時には自動では実行されない
go run ./cmd/api       # 既定は http://127.0.0.1:8001
```

フロントエンドは次の手順です。

```bash
cd frontend
npm ci
npm run dev
```

- Go依存: `server/go.mod`。追加は `go get` ではなく、事前に許可を得る
- APIのローカル設定: `server/.env`。値を読まず、必要なら利用者に確認する
- フロントのAPI URL: `frontend/.env` の `VITE_API_BASE_URL`（ローカルは `http://127.0.0.1:8001`）。形式は `frontend/.env.example` を参照する
- Swagger UI は、ローカル（`SWAGGER_ENABLED=true`）では `http://127.0.0.1:8001/docs`。**本番では無効**（`APP_ENV=production` では有効にできない）
- APIの契約は `server/openapi/openapi.json`。変更したら `cd server && go run ./cmd/openapi` で生成し直す（CIが差分を検査する）。DB問い合わせは `server/db/queries/` から `go tool sqlc generate` で生成する

## 完了前の検証

変更範囲に合うものを実際に実行し、出力を確認します。

```bash
cd server && test -z "$(gofmt -l .)" && go vet ./... && go test ./... -race -cover && go build ./cmd/api ./cmd/migrate ./cmd/openapi
cd frontend && npm run lint && npm run test && npm run build
node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs" "scripts/agent-harness/**/*.test.mjs" scripts/*.test.mjs
```

- DBが要る試験（`server/db/migrations` のDB結合試験など）は、環境変数 `TEST_DATABASE_URL` が無いとスキップされる。
- `ops/` を変更したときは、対応する試験も実行する（`bash ops/deploy_go_test.sh`、`deploy_release_test.sh`、`verify_release_test.sh`、`ops/tests/test_prune_backups.sh`。本物のDockerでの通しは `deploy_go_integration_test.sh`）。
- Windowsの `gofmt -l` は、改行がCRLFのファイルも列挙する（CIはLinuxで動くため、CIで確認する）。

失敗した検証を成功と報告しません。

## 構成

バックエンドは、機能ごとのパッケージに分けたモジュラーモノリスです（`docs/adr/0001-use-go-modular-monolith.md`）。

- `server/cmd/api/`: APIサーバーの起動点。`cmd/migrate/` はDBの移行、`cmd/openapi/` はAPI契約の生成
- `server/internal/app/`: ルーティングとミドルウェアの組み立て（RequestID → Recover → AccessLog → CORS → CSRF の順）
- `server/internal/auth/`: 登録・ログイン・ログアウト・本人確認（サーバー側セッション）
- `server/internal/recommendation/`: 記事の取得・順位付け・フィード。`interest/` は興味度とクリック学習
- `server/internal/provider/`: Qiita・Zennの取得（`qiita/`、`zenn/`）。`article/` は記事の型と検証
- `server/internal/health/`: 稼働確認（`/health/live`、`/health/ready`）
- `server/internal/store/`、`server/db/`: DBアクセス（sqlcで生成）、問い合わせ（`db/queries/`）、移行（`db/migrations/`）
- `server/internal/platform/`: 設定・ログ・DB接続・HTTPの共通部品
- `server/openapi/openapi.json`: API契約の正本（生成物）

フロントエンドの `frontend/index.html` はReactを読み込まない公開ページで、ログイン後のSPAは `/app/` 配下です。

運用の仕掛けは `ops/`（サーバーで実行するスクリプトと、その試験）と `docs/deploy/`（手順書）にあります。

## パーソナライズの重要仕様

1. タグの興味度は0〜1の小数ですが、DBの `recommend.match_int` には10000倍した整数（固定小数点）で保存します（`interest.Scale`）。
2. 記事クリック時は、全タグの興味度を0.8倍（`DecayNumerator / DecayDenominator` = 8/10）に減衰し、クリックした記事のタグへ0.2（`ClickBoost` = 2000）を加えます。上限は1です。
3. 記事取得は、興味度上位5タグを使います。QiitaとZennから並行取得し、Qiitaは直近5日、Zennは直近14日（新しいものが0件なら古いものを使う）で絞ります。順位は `タグ重みの合計 × (likes + 1)` で付け、提供元ごとに上位10件を返します。
4. タグの比較は、前後の空白を除いて小文字にそろえて行います（`interest.NormalizeTag`）。大文字小文字だけが違うタグが、DBに複数あってはいけません。
5. 外部提供元の片方だけが失敗したときは、成功した側の記事を返し、本文の `warnings` に知らせます。両方失敗したときだけエラーです。

## API・認証の重要仕様

- APIは `/api/v1/` 配下（`auth/signup`、`auth/login`、`auth/logout`、`auth/me`、`feed`、`feedback/article-clicks`）と、`/health/live`、`/health/ready` です。契約の正本は `server/openapi/openapi.json` です。
- 結果は**HTTPステータス**で表し、エラーの本文はProblem Details（RFC 9457）です。旧Python版の「本文の `status` で表す」規約は、もうありません。
- 認証は**サーバー側セッション**です（`docs/adr/0002-use-server-side-sessions.md`）。ログインで、HttpOnly・SameSite=LaxのCookie（本番は `__Host-mtp_session`、Secure付き）を発行し、DBには乱数トークンのSHA-256ハッシュだけを保存します。ログアウトで、サーバー側でも失効します。
- 更新系の要求（`/api/v1/` のPOST）には、許可されたOriginと、専用ヘッダー `X-MTP-CSRF` が必要です（CSRF対策）。
- `login` は、利用者名が存在しない場合とパスワードが違う場合を区別せず、同じ `invalid_credentials`（HTTP 401）を返します。
- パスワードは、旧Python版が作ったbcryptの形式をそのまま検証できます。
- 保護APIを変更するときは、`server/openapi/openapi.json`（生成物）と、`frontend/src/api/` の型・呼び出しを同時に更新します（CIが差分を検査します）。

## Issue・PR

- IssueとPRのタイトル・本文は、非エンジニアにも分かる平易な日本語で書く。
- 必要な場合だけ `path/to/file:line` 形式で場所を示す。
- AIが作成したPRは、人間がレビュー・承認・マージする。
- ブランチ名、コミット、PRの詳しい規約は `CONTRIBUTING.md` を正本とする。

## 関連資料

- Claude Code固有の仕掛け: `.claude/README.md`
- Codex固有の仕掛け: `.codex/README.md`
- ハーネス全体: `docs/agent-harness/README.md`
- 用語（利用者・興味度・推薦など）: `CONTEXT.md`。設計判断: `docs/adr/`
- 本番の更新（自動デプロイ）: `docs/deploy/go-auto-deploy.md`。サーバーの構築: `docs/deploy/lightsail-provisioning.md`。バックアップと復元: `docs/deploy/database-backup-and-restore.md`。Python版へ戻す緊急手順: `docs/deploy/rollback-to-python.md`
- 残タスク: `TASKS.md`
