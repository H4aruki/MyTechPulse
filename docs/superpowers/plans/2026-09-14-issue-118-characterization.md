# Issue 118 Current Behavior Characterization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Python版の認証、興味度、記事取得、クリック更新を固定入力で再現し、Go版が維持する仕様と意図的に変える旧契約を明確にする。

**Architecture:** 外部通信と本番DBを使わず、pytestのfixtureとmonkeypatchで現行サービスを特性テストする。共有JSONはrepository直下の `testdata/compatibility/` に置き、Python版とGo版が同じ入力と期待値を利用する。

**Tech Stack:** Python 3.12、pytest 9.1.1、既存FastAPI・HTTPX・Passlib

**Spec:** `docs/superpowers/specs/2026-09-14-go-backend-migration-design.md`

## Global Constraints

- 新しいPython依存は追加しない
- `.env` と `backend/.env` を読まず、テストには合成設定値だけを使う
- 外部のQiita・Zenn、本番DB、本番サーバーへ接続しない
- 個人情報、実利用者名、実パスワードハッシュをfixtureへ入れない
- `recommend.match_int` の既存値を変更しない
- 旧APIの独自 `status` とGo版のHTTP契約を混同しない
- Python版テストの削除は #128で別途許可を得るまで行わない

---

## File Map

- Create: `backend/tests/conftest.py` — import前の合成環境変数
- Create: `testdata/compatibility/qiita_articles.json` — Qiita固定応答
- Create: `testdata/compatibility/zenn_articles.json` — Zenn固定応答
- Create: `testdata/compatibility/compatibility_cases.json` — Go版と共有する期待値
- Create: `testdata/compatibility/auth.json` — 合成bcrypt互換値
- Create: `backend/tests/characterization/test_auth_contract.py` — bcryptと旧認証応答
- Create: `backend/tests/characterization/test_news_contract.py` — 期間、タグ、重複、Zennフォールバック
- Create: `backend/tests/characterization/test_click_contract.py` — 興味更新とトランザクション境界
- Modify: `backend/tests/unit/test_scoring.py` — 全丸め境界と重複タグの現行挙動
- Modify: `.github/workflows/ci.yml` — characterizationテストを既存Pythonジョブへ追加

### Task 1: 合成設定と共有fixtureを作る

**Files:**
- Create: `backend/tests/conftest.py`
- Create: `testdata/compatibility/qiita_articles.json`
- Create: `testdata/compatibility/zenn_articles.json`
- Create: `testdata/compatibility/compatibility_cases.json`
- Create: `testdata/compatibility/auth.json`

**Interfaces:**
- Consumes: 現行 `backend/app/config.py` の `DATABASE_URL`、`QIITA_ACCESS_TOKEN`、`SECRET_KEY`
- Produces: `fixture_path(name: str) -> pathlib.Path` と、Go版でも読めるUTF-8 JSON

- [ ] **Step 1: import時に実設定へ依存することを確認する**

Run:

```powershell
Remove-Item Env:DATABASE_URL -ErrorAction SilentlyContinue
Remove-Item Env:QIITA_ACCESS_TOKEN -ErrorAction SilentlyContinue
Remove-Item Env:SECRET_KEY -ErrorAction SilentlyContinue
backend/venv/Scripts/python.exe -c "import sys; sys.path.insert(0, 'backend'); import app.config"
```

Expected: 必須設定不足で失敗する。`.env` の値は表示しない。

- [ ] **Step 2: 合成設定だけを入れるconftestを書く**

```python
# backend/tests/conftest.py
import os
from pathlib import Path

os.environ.setdefault(
    "DATABASE_URL",
    "postgresql+psycopg://postgres:postgres@127.0.0.1:5432/mytechpulse_test",
)
os.environ.setdefault("QIITA_ACCESS_TOKEN", "synthetic-qiita-token")
os.environ.setdefault("SECRET_KEY", "synthetic-test-key-with-at-least-32-bytes")

FIXTURE_DIR = Path(__file__).parents[2] / "testdata" / "compatibility"


def fixture_path(name: str) -> Path:
    return FIXTURE_DIR / name
```

- [ ] **Step 3: 提供元fixtureを最小の実応答形で作る**

`qiita_articles.json`:

```json
[
  {
    "title": "Go and PostgreSQL",
    "url": "https://qiita.com/example/items/go-postgres",
    "likes_count": 4,
    "created_at": "2026-09-13T00:00:00+00:00",
    "tags": [{"name": "Go"}, {"name": "PostgreSQL"}]
  }
]
```

`zenn_articles.json`:

```json
{
  "articles": [
    {
      "title": "Go API Design",
      "path": "/example/articles/go-api",
      "liked_count": 7,
      "published_at": "2026-09-01T00:00:00.000Z"
    }
  ]
}
```

`compatibility_cases.json`:

```json
{
  "weight_scale": 10000,
  "alpha_numerator": 8,
  "alpha_denominator": 10,
  "click_boost": 2000,
  "signup_interest_initial": 1,
  "top_tag_count": 5,
  "qiita_days": 5,
  "zenn_days": 14,
  "qiita_per_tag_count": 20,
  "zenn_per_tag_count": 5,
  "provider_article_limit": 10,
  "username_max_characters": 50,
  "password_max_utf8_bytes": 72,
  "signup_tag_min_count": 1,
  "signup_tag_max_count": 128,
  "click_tag_min_count": 1,
  "click_tag_max_count": 50,
  "tag_max_characters": 50,
  "zenn_endpoint": "https://zenn.dev/api/articles"
}
```

`auth.json`:

```json
{
  "password": "synthetic-compatibility-check",
  "bcrypt_2b": "$2b$12$0i2dXOZZINjC8vcr7H7NuOStUGdS0e1t5ZOOTgiL2m0NMMdvOj4s."
}
```

- [ ] **Step 4: fixtureがJSONとして読めることを確認する**

Run:

```powershell
backend/venv/Scripts/python.exe -m json.tool testdata/compatibility/qiita_articles.json > $null
backend/venv/Scripts/python.exe -m json.tool testdata/compatibility/zenn_articles.json > $null
backend/venv/Scripts/python.exe -m json.tool testdata/compatibility/compatibility_cases.json > $null
backend/venv/Scripts/python.exe -m json.tool testdata/compatibility/auth.json > $null
```

Expected: すべてexit 0。

- [ ] **Step 5: fixture作成をコミットする**

```bash
git add backend/tests/conftest.py testdata/compatibility
git commit -m "test(migration): 移行比較用fixtureを追加" -m "Refs #118"
```

### Task 2: bcryptと旧認証契約を固定する

**Files:**
- Create: `backend/tests/characterization/test_auth_contract.py`

**Interfaces:**
- Consumes: `Hasher.get_password_hash(str) -> str`、`Hasher.verify_password(str, str) -> bool`、旧 `login_check_service`
- Produces: `$2b$` bcrypt互換、利用者不存在と誤パスワードが同じ `(2, None)` になる証拠

- [ ] **Step 1: 合成bcrypt互換テストを書く**

```python
from app.utils.hashing import Hasher


def test_passlib_hash_is_bcrypt_2b_and_verifies():
    hashed = Hasher.get_password_hash("synthetic-compatibility-check")
    assert hashed.startswith("$2b$")
    assert Hasher.verify_password("synthetic-compatibility-check", hashed)
    assert not Hasher.verify_password("wrong", hashed)
```

- [ ] **Step 2: 認証失敗が同じ契約になるテストを書く**

```python
from types import SimpleNamespace

from app.schemas.auth import LoginRequest
from app.services import auth_service


def test_unknown_user_and_wrong_password_share_response(monkeypatch):
    request = LoginRequest(username="nobody", password="wrong")
    monkeypatch.setattr(auth_service.crud.user, "get_user_by_username", lambda *_args, **_kwargs: None)
    unknown = auth_service.login_check_service(object(), request)

    hashed = auth_service.Hasher.get_password_hash("correct")
    monkeypatch.setattr(
        auth_service.crud.user,
        "get_user_by_username",
        lambda *_args, **_kwargs: SimpleNamespace(user_ID=1, user_name="nobody", password=hashed),
    )
    wrong = auth_service.login_check_service(object(), request)
    assert unknown == wrong == (2, None)
```

- [ ] **Step 3: 対象テストを実行する**

会員登録で作る興味度の初期値も固定する。

```python
from app.crud.recommend import create_recommendation


class CapturingDB:
    def __init__(self):
        self.added = []

    def add(self, value):
        self.added.append(value)


def test_signup_interest_starts_at_stored_value_one():
    db = CapturingDB()
    create_recommendation(db, user_id=1, tag_id=2)
    assert len(db.added) == 1
    assert db.added[0].match_int == 1
```

Run:

```powershell
backend/venv/Scripts/python.exe -m pytest backend/tests/characterization/test_auth_contract.py -q
```

Expected: 2 tests pass。失敗時にハッシュや秘密値を出力しない。

- [ ] **Step 4: 認証特性をコミットする**

```bash
git add backend/tests/characterization/test_auth_contract.py
git commit -m "test(auth): Python版の認証互換を固定" -m "Refs #118"
```

### Task 3: 興味度の丸めとクリック挙動を固定する

**Files:**
- Modify: `backend/tests/unit/test_scoring.py`
- Create: `backend/tests/characterization/test_click_contract.py`

**Interfaces:**
- Consumes: `calculate_new_weights(dict[str, float], list[str]) -> dict[str, float]`
- Produces: Python浮動小数の現行値、Go固定小数点の意図値、DB失敗時rollbackの証拠

- [ ] **Step 1: 浮動小数と整数式の差を明示するテストを書く**

```python
import pytest

from app.utils.scoring import calculate_new_weights


@pytest.mark.parametrize(
    ("stored", "python_decay", "integer_decay"),
    [(875, 699, 700), (1725, 1379, 1380), (10000, 8000, 8000)],
)
def test_python_rounding_is_recorded(stored, python_decay, integer_decay):
    result = calculate_new_weights({"Go": stored / 10000}, [])
    assert int(result["Go"] * 10000) == python_decay
    assert stored * 8 // 10 == integer_decay
```

- [ ] **Step 2: 重複タグと大小文字の現行挙動をテストする**

```python
def test_duplicate_and_case_variant_tags_are_distinct_in_python():
    result = calculate_new_weights({"Go": 0.5}, ["Go", "go", "Go"])
    assert result == {"Go": pytest.approx(0.8), "go": pytest.approx(0.2)}
```

このテストはGo版で重複除去・小文字照合へ意図的に変更する比較資料であり、Go版へ同じ不具合を移植しない。

- [ ] **Step 3: DB失敗時のrollbackテストを書く**

```python
from types import SimpleNamespace

from app.services import click_service


class FailingDB:
    rolled_back = False

    def commit(self):
        raise RuntimeError("synthetic failure")

    def rollback(self):
        self.rolled_back = True

    def flush(self):
        return None


def test_click_failure_rolls_back(monkeypatch):
    db = FailingDB()
    monkeypatch.setattr(click_service.crud.recommend, "get_recommendations_by_user_id", lambda *_a, **_k: [])
    monkeypatch.setattr(click_service.crud.tag, "get_tag_by_name", lambda *_a, **_k: SimpleNamespace(tag_ID=1))
    monkeypatch.setattr(click_service.crud.recommend, "update_or_create_recommendation", lambda *_a, **_k: None)
    assert click_service.update_user_weights(db, SimpleNamespace(user_ID=1), ["Go"]) is False
    assert db.rolled_back is True
```

- [ ] **Step 4: テストを実行する**

Run:

```powershell
backend/venv/Scripts/python.exe -m pytest backend/tests/unit/test_scoring.py backend/tests/characterization/test_click_contract.py -q
```

Expected: all tests pass。標準出力に合成例外だけが出ても、秘密情報は無い。

- [ ] **Step 5: 興味度特性をコミットする**

```bash
git add backend/tests/unit/test_scoring.py backend/tests/characterization/test_click_contract.py
git commit -m "test(recommend): 興味度の移行境界を固定" -m "Refs #118"
```

### Task 4: 記事取得・Zennフォールバックを固定する

**Files:**
- Create: `backend/tests/characterization/test_news_contract.py`

**Interfaces:**
- Consumes: `_build_zenn_articles_by_url(top_tags, zenn_results, since)`、`get_personalized_articles(db, user)`
- Produces: Qiita 5日、Zenn 14日、URL重複排除、Zennタグ統合、Zenn期間撤廃フォールバックの期待値

- [ ] **Step 1: Zenn変換とタグ統合のテストを書く**

```python
from datetime import datetime, timezone

from app.services.news_service import _build_zenn_articles_by_url


def test_zenn_duplicate_url_merges_search_topics():
    raw = {
        "title": "Go API Design",
        "path": "/example/articles/go-api",
        "liked_count": 7,
        "published_at": "2026-09-01T00:00:00.000Z",
    }
    result = _build_zenn_articles_by_url(
        ["Go", "API"],
        [[raw], [raw]],
        since=datetime(2026, 8, 31, tzinfo=timezone.utc),
    )
    article = result["https://zenn.dev/example/articles/go-api"]
    assert article.tags == ["Go", "API"]
    assert article.likes == 7
```

- [ ] **Step 2: 期間境界とフォールバックのテストを書く**

外部通信をしない次の補助を使い、時刻を `2026-09-14T00:00:00Z` に固定する。

```python
import asyncio
from datetime import datetime
from types import SimpleNamespace

from app.services import news_service


class FixedDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2026, 9, 14, tzinfo=tz)


class DummyAsyncClient:
    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return False


def test_provider_period_boundaries_and_zenn_fallback(monkeypatch):
    recommends = [SimpleNamespace(tag=SimpleNamespace(tag_name="Go"), match_int=5000)]
    monkeypatch.setattr(news_service.crud.recommend, "get_recommendations_by_user_id", lambda *_a, **_k: recommends)
    monkeypatch.setattr(news_service, "datetime", FixedDateTime)
    monkeypatch.setattr(news_service.httpx, "AsyncClient", DummyAsyncClient)

    async def qiita(_client, _tag):
        return [
            {"title": "excluded", "url": "https://qiita.com/example/items/excluded", "likes_count": 1, "created_at": "2026-09-09T00:00:00+00:00", "tags": [{"name": "Go"}]},
            {"title": "included", "url": "https://qiita.com/example/items/included", "likes_count": 1, "created_at": "2026-09-09T00:00:01+00:00", "tags": [{"name": "Go"}]},
        ]

    async def zenn(_client, _tag):
        return [{"title": "old fallback", "path": "/example/articles/old", "liked_count": 1, "published_at": "2026-08-01T00:00:00.000Z"}]

    monkeypatch.setattr(news_service, "fetch_qiita_articles_for_tag", qiita)
    monkeypatch.setattr(news_service, "fetch_zenn_articles_for_tag", zenn)
    result = asyncio.run(news_service.get_personalized_articles(object(), SimpleNamespace(user_ID=1)))
    assert [item.url for item in result["qiita"]] == ["https://qiita.com/example/items/included"]
    assert [item.url for item in result["zenn"]] == ["https://zenn.dev/example/articles/old"]
```

Zennの14日より新しいfixtureを使う別caseでは期間内結果が採用され、fallback用の古い候補が混ざらないことも確認する。期待値は次の形で明示する。

```python
assert [item.url for item in result["qiita"]] == [
    "https://qiita.com/example/items/go-postgres"
]
assert [item.url for item in result["zenn"]] == [
    "https://zenn.dev/example/articles/go-api"
]
```

- [ ] **Step 3: 提供元ごとのスコア差を固定する**

Go=0.5、PostgreSQL=0.25とし、QiitaではGo記事likes=1をSQL記事likes=0より上、ZennではGo記事liked_count=0をSQL記事liked_count=100より上にするfixtureを使う。結果URL順を次で固定し、Qiitaだけlikes倍率、Zennはタグ重みだけで並ぶことを証明する。

```python
assert [item.url for item in result["qiita"]] == [
    "https://qiita.com/example/items/go",
    "https://qiita.com/example/items/sql",
]
assert [item.url for item in result["zenn"]] == [
    "https://zenn.dev/example/articles/go",
    "https://zenn.dev/example/articles/sql",
]
```

- [ ] **Step 4: 対象テストを実行する**

Run:

```powershell
backend/venv/Scripts/python.exe -m pytest backend/tests/characterization/test_news_contract.py -q
```

Expected: URL、期間、順位、フォールバックの全ケースがpass。

- [ ] **Step 5: 記事特性をコミットする**

```bash
git add backend/tests/characterization/test_news_contract.py
git commit -m "test(news): Python版の記事互換を固定" -m "Refs #118"
```

### Task 5: CIへ追加してIssueをレビュー可能にする

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: Task 1〜4のpytest
- Produces: `backend-lint` ジョブ内で全Python自動テストを実行する検査

- [ ] **Step 1: 現在のCI相当テストを実行する**

```powershell
backend/venv/Scripts/python.exe -m ruff check backend
backend/venv/Scripts/python.exe -m pytest backend/tests/unit backend/tests/characterization -q
```

Expected: all pass。

- [ ] **Step 2: CIのpytest対象をディレクトリへ広げる**

```yaml
- name: Python版の移行互換を確認
  run: python -m pytest backend/tests/unit backend/tests/characterization -q
```

既存の必須ジョブ名 `バックエンドの書き方チェック` は変更しない。

- [ ] **Step 3: workflow構文と差分を確認する**

```bash
git diff --check
git diff -- .github/workflows/ci.yml backend/tests
```

Expected: 意図したテスト追加だけが表示される。

- [ ] **Step 4: CI変更をコミットする**

```bash
git add .github/workflows/ci.yml
git commit -m "ci(migration): Python互換テストを必須化" -m "Refs #118"
```

- [ ] **Step 5: ブランチをpushしてPRを作る**

PRタイトルは `test(migration): 現行APIと保存データの互換性を固定する`、本文に実行コマンド、合成データだけを使ったこと、Go版で意図的に変える契約を記載し、`Closes #118` を付ける。人間のレビュー・マージ後にIssueが閉じることを確認する。
