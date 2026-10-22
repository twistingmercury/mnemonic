"""Polling with a deadline, and unique names so tests share one stack safely."""

import time
import uuid
from collections.abc import Callable

from mnemonic_e2e.api import ApiClient, Pattern

ENRICH_TIMEOUT_S = 30.0


def until[T](
    fn: Callable[[], T],
    what: str,
    timeout_s: float = 30.0,
    interval_s: float = 0.25,
    done: Callable[[T], bool] = bool,
) -> T:
    """Call fn until done(value) holds (default: value is truthy) and return that value.

    fn runs at least once. On timeout the TimeoutError names what was awaited
    and the last value fn returned, so a failed wait explains itself.
    """
    deadline = time.monotonic() + timeout_s
    while True:
        value = fn()
        if done(value):
            return value

        if time.monotonic() >= deadline:
            raise TimeoutError(f"timed out after {timeout_s}s waiting for {what}; last observed: {value!r}")

        time.sleep(interval_s)


def unique_name(prefix: str) -> str:
    """Return prefix plus 12 hex digits: lowercase, hyphenated, valid as a pattern name."""
    return f"{prefix}-{uuid.uuid4().hex[:12]}"


def enriched(api: ApiClient, pattern_id: str, timeout_s: float = ENRICH_TIMEOUT_S) -> Pattern:
    """Poll until the pattern is enriched and return it; a failed enrichment ends the wait and the test."""

    def fetch() -> Pattern:
        response = api.get(pattern_id)
        assert response.status == 200, f"GET pattern {pattern_id}: status {response.status}\nbody: {response.text}"
        return response.pattern()

    pattern = until(
        fetch,
        f"pattern {pattern_id} to be enriched",
        timeout_s,
        done=lambda p: p.enrichment_status in ("enriched", "failed"),
    )
    assert pattern.enrichment_status == "enriched", (
        f"pattern {pattern_id} enrichment failed: {pattern.enrichment_error}"
    )
    return pattern
