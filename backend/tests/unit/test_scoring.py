import json

import pytest

from backend.app.utils.scoring import ALPHA, calculate_new_weights
from backend.tests.conftest import fixture_path


def load_compatibility_cases() -> dict[str, int]:
    return json.loads(fixture_path("compatibility_cases.json").read_text(encoding="utf-8"))


def test_existing_and_new_tags_are_updated() -> None:
    current = {"Python": 0.5, "SQL": 0.3}

    result = calculate_new_weights(current, ["Python", "FastAPI"])

    assert result == pytest.approx(
        {
            "Python": 0.5 * ALPHA + (1 - ALPHA),
            "SQL": 0.3 * ALPHA,
            "FastAPI": 1 - ALPHA,
        }
    )


def test_all_existing_tags_decay_without_clicked_tags() -> None:
    result = calculate_new_weights({"Python": 0.5, "SQL": 0.25}, [])

    assert result == pytest.approx({"Python": 0.4, "SQL": 0.2})


def test_input_weights_are_not_modified() -> None:
    current = {"Python": 0.5}

    calculate_new_weights(current, ["Python"])

    assert current == {"Python": 0.5}


@pytest.mark.parametrize(
    ("stored", "python_decay", "integer_decay"),
    [(875, 699, 700), (1725, 1379, 1380), (10000, 8000, 8000)],
)
def test_python_rounding_is_recorded(stored: int, python_decay: int, integer_decay: int) -> None:
    cases = load_compatibility_cases()
    result = calculate_new_weights({"Go": stored / cases["weight_scale"]}, [])

    assert int(result["Go"] * cases["weight_scale"]) == python_decay
    assert stored * cases["alpha_numerator"] // cases["alpha_denominator"] == integer_decay


def test_duplicate_and_case_variant_tags_are_distinct_in_python() -> None:
    cases = load_compatibility_cases()
    result = calculate_new_weights({"Go": 0.5}, ["Go", "go", "Go"])

    assert result == {"Go": pytest.approx(0.8), "go": pytest.approx(0.2)}
    assert int(result["go"] * cases["weight_scale"]) == cases["click_boost"] - 1
