from types import SimpleNamespace

from backend.app.services import click_service


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
