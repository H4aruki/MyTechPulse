# MyTechPulse エージェント作業ルール

このファイルは、Codex と Claude Code が共通で参照するプロジェクト固有ルールの正本です。
共通ルールを変更するときは、このファイルを先に更新してください。

## プロジェクト概要

MyTechPulse は、Qiita と Zenn から利用者の興味タグに合う記事を集め、優先順位を付けて届ける技術ニュースアプリです。

- フロントエンド: Vite、React、TypeScript。`frontend/`
- バックエンド: FastAPI、SQLAlchemy、PostgreSQL 17。`backend/app/`
- 公開: `main` への反映をきっかけに `.github/workflows/ci.yml` が自動実行する

## 作業開始時

1. このファイルと、対象ディレクトリにある追加の `AGENTS.md` を読む。
2. 対象作業に合う `.agents/skills/` または `.claude/skills/` の Skill を読む。
3. `git status --short --branch` で現在地と未コミット変更を確認する。
4. 新しい変更を始める場合は、`main` で `git pull --ff-only` を実行してから `<type>/<kebab-case>` の作業ブランチを作る。
5. 既存の未コミット変更は利用者のものとして扱い、勝手に戻したり上書きしたりしない。

## 禁止事項と許可が必要な操作

- `.env` と `backend/.env` を読まない。項目名は `.env.example` で確認する。
- `git push --force`、`git reset --hard`、`git clean` など、履歴や作業内容を失う操作をしない。
- `main` へ直接 push しない。
- PR の承認とマージをしない。最終判断は人間が行う。
- DBボリュームを削除しない。停止だけなら `docker compose down` を使う。
- 本番サーバーへ直接接続せず、手動公開もしない。
- 既存ファイルや既存データを削除する前に、対象・役割・理由・影響を説明して許可を得る。
- ライブラリやフレームワークを追加する前に、名称・理由・影響を説明して許可を得る。
- GitHub操作以外の外部公開・送信は、内容を準備してから明示的な許可を得る。
- 課金やサブスクリプション登録をしない。

機械的な防御は `.claude/settings.json`、`.claude/hooks/`、`.codex/` にあります。文章と防御設定は同じ方針を保ってください。

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

PostgreSQLを先に起動します。

```bash
docker compose up -d db
```

バックエンドは `backend/` から実行します。

```bash
cd backend
python init_db.py
uvicorn app.main:app --reload
```

フロントエンドは次の手順です。

```bash
cd frontend
npm ci
npm run dev
```

- Python依存: `pip install -r requirements.txt`
- APIのローカル設定: `backend/.env`。値を読まず、必要なら利用者に確認する
- フロントのAPI URL: `frontend/.env` の `VITE_API_BASE_URL`。形式は `.env.example` を参照する
- `backend/tests/test_SQL.py` は手動のDB接続確認であり、自動テストではない

## 完了前の検証

変更範囲に合うものを実際に実行し、出力を確認します。

```bash
ruff check backend
cd frontend && npm run lint && npm run build
node --test ".claude/hooks/**/*.test.mjs" ".codex/hooks/**/*.test.mjs" "scripts/agent-harness/**/*.test.mjs"
```

`ruff` が PATH に無い場合は、Windowsでは `backend/venv/Scripts/ruff.exe`、macOS/Linuxでは `backend/venv/bin/ruff` を使います。失敗した検証を成功と報告しません。

## 構成

バックエンドの基本的な流れは `routes` → `services` → `crud` → `models` です。

- `backend/app/routes/`: APIの入口
- `backend/app/services/`: 業務処理。記事取得の中心は `news_service.py`
- `backend/app/crud/`: DBアクセス
- `backend/app/models/`: SQLAlchemyモデル
- `backend/app/schemas/`: APIの入出力型
- `backend/app/utils/scoring.py`: 興味度の更新計算

フロントエンドの `frontend/index.html` はReactを読み込まない公開ページで、ログイン後のSPAは `/app/` 配下です。

## パーソナライズの重要仕様

1. タグの興味度は0〜1の小数ですが、DBの `recommend.match_int` には10000倍した整数で保存します。services層で相互変換します。
2. 記事クリック時は全タグを `ALPHA = 0.8` で減衰し、記事のタグへ `(1 - ALPHA)` を加えます。
3. 記事取得は興味度上位5タグを使います。QiitaとZennから並行取得し、直近5日とタグで絞り、`タグ重みの合計 × (likes + 1)` で順位を付けます。
4. Zennが0件なら日付だけで絞るフォールバックがあります。タグ比較は小文字化します。

## API・認証の重要仕様

- 多くのAPIはHTTPステータスではなく、レスポンス本文の `status` で結果を表します。
- `login_check` は利用者名の存在とパスワード不一致を区別せず、どちらも `status = 2` と同じ文言を返します。
- `status` の意味を変える場合は、`frontend/src/api/types.ts` の `ApiStatus` も同時に更新します。
- 認証はJWTのHS256です。フロントは `localStorage` の `access_token` をBearerトークンとして送ります。
- 保護APIの利用者特定は `backend/app/dependencies.py` の `get_current_user` が行います。
- トークンの欠如・不正・期限切れは、このアプリの本文`status`規約の例外としてHTTP 401です。
- サーバー側のトークン失効機構はなく、ログアウトはフロント側のトークン破棄だけです。

## Issue・PR

- IssueとPRのタイトル・本文は、非エンジニアにも分かる平易な日本語で書く。
- 必要な場合だけ `path/to/file:line` 形式で場所を示す。
- AIが作成したPRは、人間がレビュー・承認・マージする。
- ブランチ名、コミット、PRの詳しい規約は `CONTRIBUTING.md` を正本とする。

## 関連資料

- Claude Code固有の仕掛け: `.claude/README.md`
- Codex固有の仕掛け: `.codex/README.md`
- ハーネス全体: `docs/agent-harness/README.md`
- 残タスク: `TASKS.md`
