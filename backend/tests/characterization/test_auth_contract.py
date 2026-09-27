from types import SimpleNamespace

from backend.app.crud.recommend import create_recommendation
from backend.app.schemas.auth import LoginRequest
from backend.app.services import auth_service
from backend.app.utils.hashing import Hasher


class CapturingDB:
    def __init__(self) -> None:
        self.added: list[object] = []

    def add(self, value: object) -> None:
        self.added.append(value)


def test_passlib_hash_is_bcrypt_2b_and_verifies() -> None:
    password = "synthetic-compatibility-check"

    hashed = Hasher.get_password_hash(password)

    assert hashed.startswith("$2b$")
    assert Hasher.verify_password(password, hashed)
    assert not Hasher.verify_password("wrong", hashed)


def test_unknown_user_and_wrong_password_share_response(monkeypatch) -> None:
    request = LoginRequest(username="nobody", password="wrong")
    monkeypatch.setattr(
        auth_service.crud.user,
        "get_user_by_username",
        lambda *_args, **_kwargs: None,
    )
    unknown = auth_service.login_check_service(object(), request)

    hashed = auth_service.Hasher.get_password_hash("correct")
    monkeypatch.setattr(
        auth_service.crud.user,
        "get_user_by_username",
        lambda *_args, **_kwargs: SimpleNamespace(user_ID=1, user_name="nobody", password=hashed),
    )
    wrong = auth_service.login_check_service(object(), request)

    assert unknown == wrong == (2, None)


def test_signup_interest_starts_at_stored_value_one() -> None:
    db = CapturingDB()

    create_recommendation(db, user_id=1, tag_id=2)

    assert len(db.added) == 1
    assert db.added[0].match_int == 1
