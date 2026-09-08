# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

MyTechPulse — Qiita/Zennからユーザーの興味タグに基づいて記事を収集・スコアリングして届けるパーソナライズ技術ニュースアプリ。

- **フロントエンド**: Vite + React + TypeScript。`frontend/`。LP（`frontend/index.html`）はReactを読み込まない静的HTML、ログイン後は `/app/` 配下のSPA
- **バックエンド**: FastAPI + SQLAlchemy + PostgreSQL 17（Dockerコンテナ）。`backend/app/`

## やってはいけないこと

以下は `.claude/settings.json` と `.claude/hooks/` で機械的に止めている。**文章と設定は1対1で対応させているので、片方だけ変えないこと。**

- **接続情報ファイル（`.env`）を読まない・持ち出さない・記録に加えない。** 項目名を知りたいときは `.env.example` を見る。値そのものが必要な作業はオーナーに依頼する
- **履歴を強制的に上書きしない。** 他のメンバーの作業が消える。取り込み済みの内容を直したいときは新しいコミットを積む
- **`main` へ直接反映しない。** `main` へ入ると本番公開まで自動で走る（`.github/workflows/ci.yml`）。必ずプルリクエスト経由で取り込む
- **プルリクエストを承認・取り込みしない。** 判断は人間だけが行う。準備ができたことを報告して待つ
- **データの入れ物ごと消さない。** データベースの中身が丸ごと失われる。止めるだけなら `docker compose down`
- **本番サーバーへ直接つながない・手動で公開しない。** 公開は `main` に取り込まれたときの自動処理に一本化してある

仕掛けの詳しい中身と、その限界は `.claude/README.md` にある。

## 開発コマンド

前提: `docker compose up -d db` でPostgreSQLコンテナを起動しておく（XAMPPは使わない）。DB `mytechpulse` はコンテナ初回起動時に `POSTGRES_DB` が作るため、`init_db.py` はテーブル作成のみを行う。`.env` は `app/config.py` が絶対パス（`backend/.env`）で読むため、リポジトリルート/`backend/` どちらから起動しても設定読み込みは失敗しない。ただし以下のコマンド例は `backend/` から実行する想定。

```bash
cd backend
python init_db.py                  # モデル定義からテーブルを作成（冪等）
uvicorn app.main:app --reload      # APIサーバー起動（http://127.0.0.1:8000）
```

API込みで丸ごと動かす場合は `docker compose up -d --build`（`backend/entrypoint.sh` が `init_db.py` を実行してから uvicorn を起動する）。

フロントエンドは `cd frontend && npm install && npm run dev` で起動する（http://localhost:5173）。CORS許可オリジンは `app/main.py` に列挙されている。APIのURLは `frontend/.env` の `VITE_API_BASE_URL` で切り替える（`.env.example` 参照）。フロント側の詳細は `frontend/README.md` にある。

- 依存: `pip install -r requirements.txt`（venvは `backend/venv/` にある）
- テスト: pytestスイートは無い。`backend/tests/test_SQL.py` は手動実行のDB接続確認スクリプトのみ
- 設定: `backend/.env` に `DATABASE_URL`、`QIITA_ACCESS_TOKEN`、`SECRET_KEY`（JWT署名鍵。未設定だと起動失敗する）が必要（pydantic-settingsの `app/config.py` が読む）

## 作業の進め方

1. **課題（GitHub Issue）を先に立てる。** 着手してから立てない。課題は GitHub Issues（H4aruki/MyTechPulse）で `gh` コマンド経由で管理する
2. **`main` から作業用の枝を切る。** 枝の名前は `<種別>/<内容>`（例: `fix/login-error`）
3. **プルリクエストを作る。** 送り先は枝であって、コミットを直接積む場所ではない
4. **取り込みはオーナーが判断する**

## 完了と言う前にやること

**実際にコマンドを実行して、その出力を確認してから報告する。** 実行していないものを「動きました」と言わない。

```bash
ruff check backend                                  # サーバー側
cd frontend && npm run lint && npm run build        # 画面側（型チェックも兼ねる）
node --test ".claude/hooks/**/*.test.mjs"           # 設定まわりの仕掛け
```

`ruff` はどこからでも呼べる状態になっていないことがある。その場合は `backend/venv/Scripts/ruff.exe`（Windows）または `backend/venv/bin/ruff` を直接指定する。

失敗しても構わない。**失敗しているならその内容をそのまま報告する。**

なお、このリポジトリには動作を確かめる自動テストがまだ無い。上記で確認できるのは書き方と型と組み立てまでで、動作が正しいかどうかは確認できない。

## Issue・PR作成時の文章表現

Issue・PRのタイトルと本文は、専門用語・関数名・変数名・クラス名の使用を極力避け、平易な日本語で書く。「何が起きているか／何をしたいか」を非エンジニアでも読める言葉で説明する。コード上の具体的な箇所を示す必要がある場合のみ、`file_path:line_number` の形式で参照する程度に留め、関数名を文中で多用しない。

## アーキテクチャ

バックエンドはレイヤード構成。リクエストは routes → services → crud → models の順に流れる:

- `app/routes/` — エンドポイント定義のみ。`auth`（/auth）、`news`（/news）、`click`（/article）
- `app/services/` — ビジネスロジック。`news_service.py` が中核
- `app/crud/` — DBアクセス（user / tag / recommend）
- `app/models/` — SQLAlchemyモデル。`recommend` テーブルが user↔tag の中間テーブルで、`match_int` に興味の重みを持つ
- `app/schemas/` — Pydanticのリクエスト/レスポンス型
- `app/utils/scoring.py` — 重み更新アルゴリズム

### パーソナライズの仕組み（複数ファイルにまたがる中核ロジック）

1. **重みの保存形式**: タグごとの興味の重みは float（0〜1）だが、DBの `recommend.match_int` には **10000倍した整数** で保存する。services層で `match_int / 10000.0` ⇔ `int(weight * 10000)` の変換を行う。この変換を忘れると桁が壊れる
2. **クリック学習** (`click_service.py` + `utils/scoring.py`): 記事クリック時に全タグの重みを `ALPHA = 0.8` で指数減衰させ、クリック記事のタグに `(1 - ALPHA)` を加算
3. **記事取得** (`news_service.py`): ユーザーの重み上位5タグでQiita API、ZennはAPI 3ページ分を並行取得（httpx + asyncio.gather）→ ユーザータグを含む・直近5日以内の記事に絞り込み → `score = Σ(タグ重み) × (likes + 1)` でスコアリング → ソース別に上位10件を返す。Zennが0件の場合は日付フィルターのみのフォールバックあり。タグ比較は小文字化して行う

### API規約

エンドポイントはHTTPエラーではなくレスポンスボディの `status`（int）で結果を返すのが基本（例: login_check は 1=成功、2=認証失敗）。ユーザー名列挙攻撃を防ぐため、login_check はユーザー不存在とパスワード不一致を区別せず同じ `status`（2）・同じエラーメッセージを返す。フロントは `frontend/src/api/types.ts` の `ApiStatus` でこの値を型として持ち分岐しているので、値の意味を変える場合は両側を揃えること。

認証はJWT（HS256）。ログイン/サインアップ成功時に `access_token` を発行し、フロントは `localStorage` の `access_token` を `Authorization: Bearer <token>` ヘッダーとして保護対象エンドポイント（`/news/personal_news`, `/article/click`）に渡す。ユーザー特定は `app/dependencies.py` の `get_current_user`（`Depends`）が行い、トークン欠如・不正・期限切れは既存のstatus規約の例外として `HTTPException(401)` を返す。サーバー側のトークン失効機構は無く、ログアウトはクライアント側の `access_token` 破棄のみ。
