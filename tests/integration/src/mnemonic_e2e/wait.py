"""Polling with a deadline, and unique names so tests share one stack safely."""

import time
import uuid
from collections.abc import Callable


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
