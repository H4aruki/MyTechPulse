import json
from types import SimpleNamespace

from backend.app.schemas.article import ClickArticleRequest
from backend.app.services import click_service
from backend.tests.conftest import fixture_path


class FailingDB:
    def __init__(self) -> None:
        self.rolled_back = False

    def commit(self) -> None:
        raise RuntimeError("synthetic failure")

    def rollback(self) -> None:
        self.rolled_back = True

    def flush(self) -> None:
        return None


def test_click_failure_rolls_back(monkeypatch) -> None:
    db = FailingDB()
    monkeypatch.setattr(
        click_service.crud.recommend,
        "get_recommendations_by_user_id",
        lambda *_args, **_kwargs: [],
    )
    monkeypatch.setattr(
        click_service.crud.tag,
        "get_tag_by_name",
        lambda *_args, **_kwargs: SimpleNamespace(tag_ID=1),
    )
    monkeypatch.setattr(
        click_service.crud.recommend,
        "update_or_create_recommendation",
        lambda *_args, **_kwargs: None,
    )

    assert click_service.update_user_weights(db, SimpleNamespace(user_ID=1), ["Go"]) is False
    assert db.rolled_back is True


def test_legacy_click_schema_accepts_values_beyond_go_input_limits() -> None:
    cases = json.loads(fixture_path("compatibility_cases.json").read_text(encoding="utf-8"))
    oversized_tags = ["x"] * (cases["click_tag_max_count"] + 1)
    oversized_tag = "x" * (cases["tag_max_characters"] + 1)

    empty_click = ClickArticleRequest(tags=[])
    oversized_click = ClickArticleRequest(tags=oversized_tags + [oversized_tag])

    assert empty_click.tags == []
    assert len(empty_click.tags) == cases["click_tag_min_count"] - 1
    assert len(oversized_click.tags) == cases["click_tag_max_count"] + 2
