import asyncio
import json
from datetime import datetime, timedelta, timezone
from types import SimpleNamespace

from backend.app.services import news_service
from backend.app.services.news_service import _build_zenn_articles_by_url
from backend.tests.conftest import fixture_path


class FixedDateTime(datetime):
    @classmethod
    def now(cls, tz=None):
        return cls(2026, 9, 14, tzinfo=tz)


class DummyAsyncClient:
    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return False


class RecordingResponse:
    def raise_for_status(self) -> None:
        return None

    def json(self) -> dict[str, list]:
        return {"articles": []}


class RecordingAsyncClient:
    def __init__(self) -> None:
        self.requests = []

    async def get(self, url, **kwargs):
        self.requests.append((url, kwargs))
        return RecordingResponse()


def load_fixture(name: str):
    return json.loads(fixture_path(name).read_text(encoding="utf-8"))


def load_compatibility_cases() -> dict[str, int | str]:
    return load_fixture("compatibility_cases.json")


def test_zenn_duplicate_url_merges_search_topics() -> None:
    raw = load_fixture("zenn_articles.json")["articles"][0]

    result = _build_zenn_articles_by_url(
        ["Go", "API"],
        [[raw], [raw]],
        since=datetime(2026, 8, 31, tzinfo=timezone.utc),
    )

    article = result["https://zenn.dev/example/articles/go-api"]
    assert article.tags == ["Go", "API"]
    assert article.likes == 7


def test_provider_request_shapes_match_shared_compatibility_cases() -> None:
    cases = load_compatibility_cases()
    client = RecordingAsyncClient()

    asyncio.run(news_service.fetch_qiita_articles_for_tag(client, "Go"))
    asyncio.run(news_service.fetch_zenn_articles_for_tag(client, "Go"))

    assert client.requests[0][0].endswith("/tags/Go/items")
    assert client.requests[0][1]["params"] == {"page": 1, "per_page": cases["qiita_per_tag_count"]}
    assert client.requests[1] == (
        cases["zenn_endpoint"],
        {"params": {"topicname": "go", "count": cases["zenn_per_tag_count"]}},
    )


def configure_provider_mocks(monkeypatch, recommends, qiita, zenn) -> None:
    monkeypatch.setattr(
        news_service.crud.recommend,
        "get_recommendations_by_user_id",
        lambda *_args, **_kwargs: recommends,
    )
    monkeypatch.setattr(news_service, "datetime", FixedDateTime)
    monkeypatch.setattr(news_service.httpx, "AsyncClient", DummyAsyncClient)
    monkeypatch.setattr(news_service, "fetch_qiita_articles_for_tag", qiita)
    monkeypatch.setattr(news_service, "fetch_zenn_articles_for_tag", zenn)


def test_provider_period_boundaries_and_zenn_fallback(monkeypatch) -> None:
    cases = load_compatibility_cases()
    recommends = [SimpleNamespace(tag=SimpleNamespace(tag_name="Go"), match_int=5000)]
    qiita_boundary = datetime(2026, 9, 14, tzinfo=timezone.utc) - timedelta(
        days=cases["qiita_days"]
    )

    async def qiita(_client, _tag):
        return [
            {
                "title": "excluded",
                "url": "https://qiita.com/example/items/excluded",
                "likes_count": 1,
                "created_at": qiita_boundary.isoformat(),
                "tags": [{"name": "Go"}],
            },
            {
                "title": "included",
                "url": "https://qiita.com/example/items/included",
                "likes_count": 1,
                "created_at": (qiita_boundary + timedelta(seconds=1)).isoformat(),
                "tags": [{"name": "Go"}],
            },
        ]

    async def zenn(_client, _tag):
        return [
            {
                "title": "old fallback",
                "path": "/example/articles/old",
                "liked_count": 1,
                "published_at": "2026-08-01T00:00:00.000Z",
            }
        ]

    configure_provider_mocks(monkeypatch, recommends, qiita, zenn)

    result = asyncio.run(
        news_service.get_personalized_articles(object(), SimpleNamespace(user_ID=1))
    )

    assert [item.url for item in result["qiita"]] == ["https://qiita.com/example/items/included"]
    assert [item.url for item in result["zenn"]] == ["https://zenn.dev/example/articles/old"]


def test_period_results_prevent_zenn_fallback(monkeypatch) -> None:
    cases = load_compatibility_cases()
    recommends = [SimpleNamespace(tag=SimpleNamespace(tag_name="Go"), match_int=5000)]
    qiita_articles = load_fixture("qiita_articles.json")
    zenn_articles = load_fixture("zenn_articles.json")["articles"]

    async def qiita(_client, _tag):
        return qiita_articles

    async def zenn(_client, _tag):
        return zenn_articles + [
            {
                "title": "old fallback",
                "path": "/example/articles/old",
                "liked_count": 1,
                "published_at": (
                    datetime(2026, 9, 14, tzinfo=timezone.utc)
                    - timedelta(days=cases["zenn_days"] + 1)
                )
                .isoformat()
                .replace("+00:00", "Z"),
            }
        ]

    configure_provider_mocks(monkeypatch, recommends, qiita, zenn)

    result = asyncio.run(
        news_service.get_personalized_articles(object(), SimpleNamespace(user_ID=1))
    )

    assert [item.url for item in result["qiita"]] == ["https://qiita.com/example/items/go-postgres"]
    assert [item.url for item in result["zenn"]] == ["https://zenn.dev/example/articles/go-api"]


def test_provider_specific_scoring_and_top_five_tags(monkeypatch) -> None:
    cases = load_compatibility_cases()
    recommends = [
        SimpleNamespace(tag=SimpleNamespace(tag_name="Go"), match_int=5000),
        SimpleNamespace(tag=SimpleNamespace(tag_name="PostgreSQL"), match_int=2500),
        SimpleNamespace(tag=SimpleNamespace(tag_name="Python"), match_int=2400),
        SimpleNamespace(tag=SimpleNamespace(tag_name="Rust"), match_int=2300),
        SimpleNamespace(tag=SimpleNamespace(tag_name="Java"), match_int=2200),
        SimpleNamespace(tag=SimpleNamespace(tag_name="Kotlin"), match_int=2100),
    ]
    fetched_qiita_tags = []
    fetched_zenn_tags = []

    async def qiita(_client, tag):
        fetched_qiita_tags.append(tag)
        if tag == "Go":
            return [
                {
                    "title": "Go",
                    "url": "https://qiita.com/example/items/go",
                    "likes_count": 1,
                    "created_at": "2026-09-13T00:00:00+00:00",
                    "tags": [{"name": "Go"}],
                }
            ]
        if tag == "PostgreSQL":
            return [
                {
                    "title": "SQL",
                    "url": "https://qiita.com/example/items/sql",
                    "likes_count": 0,
                    "created_at": "2026-09-13T00:00:00+00:00",
                    "tags": [{"name": "PostgreSQL"}],
                }
            ]
        return []

    async def zenn(_client, tag):
        fetched_zenn_tags.append(tag)
        if tag == "Go":
            return [
                {
                    "title": "Go",
                    "path": "/example/articles/go",
                    "liked_count": 0,
                    "published_at": "2026-09-13T00:00:00.000Z",
                }
            ]
        if tag == "PostgreSQL":
            return [
                {
                    "title": "SQL",
                    "path": "/example/articles/sql",
                    "liked_count": 100,
                    "published_at": "2026-09-13T00:00:00.000Z",
                }
            ]
        return []

    configure_provider_mocks(monkeypatch, recommends, qiita, zenn)

    result = asyncio.run(
        news_service.get_personalized_articles(object(), SimpleNamespace(user_ID=1))
    )

    expected_tags = ["Go", "PostgreSQL", "Python", "Rust", "Java"]
    assert fetched_qiita_tags == expected_tags[: cases["top_tag_count"]]
    assert fetched_zenn_tags == expected_tags[: cases["top_tag_count"]]
    assert [item.url for item in result["qiita"]] == [
        "https://qiita.com/example/items/go",
        "https://qiita.com/example/items/sql",
    ]
    assert [item.url for item in result["zenn"]] == [
        "https://zenn.dev/example/articles/go",
        "https://zenn.dev/example/articles/sql",
    ]


def test_provider_results_are_limited_by_shared_case(monkeypatch) -> None:
    cases = load_compatibility_cases()
    recommends = [SimpleNamespace(tag=SimpleNamespace(tag_name="Go"), match_int=5000)]
    articles = range(cases["provider_article_limit"] + 1)

    async def qiita(_client, _tag):
        return [
            {
                "title": f"Qiita {number}",
                "url": f"https://qiita.com/example/items/{number}",
                "likes_count": 0,
                "created_at": "2026-09-13T00:00:00+00:00",
                "tags": [{"name": "Go"}],
            }
            for number in articles
        ]

    async def zenn(_client, _tag):
        return [
            {
                "title": f"Zenn {number}",
                "path": f"/example/articles/{number}",
                "liked_count": 0,
                "published_at": "2026-09-13T00:00:00.000Z",
            }
            for number in articles
        ]

    configure_provider_mocks(monkeypatch, recommends, qiita, zenn)

    result = asyncio.run(
        news_service.get_personalized_articles(object(), SimpleNamespace(user_ID=1))
    )

    assert len(result["qiita"]) == cases["provider_article_limit"]
    assert len(result["zenn"]) == cases["provider_article_limit"]
