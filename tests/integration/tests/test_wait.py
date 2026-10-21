import re

import pytest

from mnemonic_e2e.wait import unique_name, until


def test_until_returns_the_first_truthy_value() -> None:
    values = iter([None, 0, "ready", "later"])

    assert until(lambda: next(values), "a value", timeout_s=1, interval_s=0.01) == "ready"


def test_until_reports_the_last_observed_value_and_the_timeout() -> None:
    with pytest.raises(TimeoutError) as info:
        until(lambda: 0, "the answer", timeout_s=0.1, interval_s=0.02)

    message = str(info.value)
    assert "the answer" in message
    assert "0.1s" in message
    assert "last observed: 0" in message


def test_until_checks_once_even_when_the_timeout_is_zero() -> None:
    assert until(lambda: "now", "now", timeout_s=0, interval_s=0.01) == "now"


def test_unique_name_is_a_valid_pattern_name_and_differs_per_call() -> None:
    first, second = unique_name("e2e"), unique_name("e2e")

    assert first != second
    assert re.fullmatch(r"e2e-[0-9a-f]{12}", first)


def test_until_accepts_a_custom_done_predicate_and_reports_the_observed_state() -> None:
    statuses = iter(["pending", "enriched"])

    assert (
        until(
            lambda: next(statuses),
            "enrichment",
            1,
            0.01,
            done=lambda s: s == "enriched",
        )
        == "enriched"
    )

    with pytest.raises(TimeoutError, match="last observed: 'pending'"):
        until(lambda: "pending", "enrichment", 0.1, 0.02, done=lambda s: s == "enriched")
