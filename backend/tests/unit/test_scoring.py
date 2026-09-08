import pytest

from backend.app.utils.scoring import ALPHA, calculate_new_weights


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
