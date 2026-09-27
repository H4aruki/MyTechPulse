import json
from types import SimpleNamespace

from backend.app.crud.recommend import create_recommendation
from backend.app.schemas.auth import LoginRequest, UserCreateRequest
from backend.app.services import auth_service
from backend.app.utils.hashing import Hasher
from backend.tests.conftest import fixture_path


class CapturingDB:
    def __init__(self) -> None:
        self.added: list[object] = []

    def add(self, value: object) -> None:
        self.added.append(value)


def test_passlib_hash_is_bcrypt_2b_and_verifies() -> None:
    auth_fixture = json.loads(fixture_path("auth.json").read_text(encoding="utf-8"))
    password = auth_fixture["password"]

    hashed = Hasher.get_password_hash(password)

    assert hashed.startswith("$2b$")
    assert Hasher.verify_password(password, hashed)
    assert not Hasher.verify_password("wrong", hashed)
    assert Hasher.verify_password(password, auth_fixture["bcrypt_2b"])


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
    cases = json.loads(fixture_path("compatibility_cases.json").read_text(encoding="utf-8"))
    db = CapturingDB()

    create_recommendation(db, user_id=1, tag_id=2)

    assert len(db.added) == 1
    assert db.added[0].match_int == cases["signup_interest_initial"]


def test_legacy_auth_schemas_accept_values_beyond_go_input_limits() -> None:
    cases = json.loads(fixture_path("compatibility_cases.json").read_text(encoding="utf-8"))
    username = "u" * (cases["username_max_characters"] + 1)
    password = "p" * (cases["password_max_utf8_bytes"] + 1)
    oversized_tags = ["x"] * (cases["signup_tag_max_count"] + 1)
    oversized_tag = "x" * (cases["tag_max_characters"] + 1)

    login = LoginRequest(username=username, password=password)
    empty_signup = UserCreateRequest(
        newusername=username,
        newpassword=password,
        favoritetags=[],
    )
    oversized_signup = UserCreateRequest(
        newusername=username,
        newpassword=password,
        favoritetags=oversized_tags + [oversized_tag],
    )

    assert login.username == username
    assert empty_signup.favoritetags == []
    assert len(empty_signup.favoritetags) == cases["signup_tag_min_count"] - 1
    assert len(oversized_signup.favoritetags) == cases["signup_tag_max_count"] + 2
