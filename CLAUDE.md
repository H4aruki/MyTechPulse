# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

MyTechPulse — Qiita/Zennからユーザーの興味タグに基づいて記事を収集・スコアリングして届けるパーソナライズ技術ニュースアプリ。

- **フロントエンド**: Vite + React + TypeScript。`frontend/`。LP（`frontend/index.html`）はReactを読み込まない静的HTML、ログイン後は `/app/` 配下のSPA
- **バックエンド**: FastAPI + SQLAlchemy + MySQL（XAMPP経由）。`backend/app/`

## 開発コマンド

前提: XAMPPでMySQLを起動しておく。`.env` は `app/config.py` が絶対パス（`backend/.env`）で読むため、リポジトリルート/`backend/` どちらから起動しても設定読み込みは失敗しない。ただし以下のコマンド例は `backend/` から実行する想定。

```bash
cd backend
python init_db.py                  # DB "mytechpulse" とテーブルを作成（初回のみ）
uvicorn app.main:app --reload      # APIサーバー起動（http://127.0.0.1:8000）
```

フロントエンドは `cd frontend && npm install && npm run dev` で起動する（http://localhost:5173）。CORS許可オリジンは `app/main.py` に列挙されている。APIのURLは `frontend/.env` の `VITE_API_BASE_URL` で切り替える（`.env.example` 参照）。フロント側の詳細は `frontend/README.md` にある。

- 依存: `pip install -r requirements.txt`（venvは `backend/venv/` にある）
- テスト: pytestスイートは無い。`backend/tests/test_SQL.py` は手動実行のDB接続確認スクリプトのみ
- 設定: `backend/.env` に `DATABASE_URL`、`QIITA_ACCESS_TOKEN`、`SECRET_KEY`（JWT署名鍵。未設定だと起動失敗する）が必要（pydantic-settingsの `app/config.py` が読む）

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
