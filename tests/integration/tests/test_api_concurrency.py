"""Concurrent API writes, and the single RELATED_TO edge per pair under concurrent enrichment.

These need the running compose stack. Every test makes its own patterns with
unique names and tokens, so the tests share one stack in any order. Each write
runs on its own thread and a barrier releases them together, so the requests
overlap instead of running one after the other.
"""

import threading
from collections import Counter
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor
from functools import partial
from itertools import combinations

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.api import ApiClient, ApiResponse, pattern_payload
from mnemonic_e2e.db import Graph
from mnemonic_e2e.tokens import TokenPool

# The create_with fixture (conftest.py): payload overrides -> the create response.
CreateWith = Callable[..., ApiResponse]


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    # Every word the test patterns have in common, tags and names included.
    return TokenPool(related_content("") + " integ api")


def related_content(token: str) -> str:
    """Single-chunk content whose concepts the stub extracts; patterns with one token share every concept."""
    return f"## Overview\n\nConcurrency test pattern.\n\nConcepts: {token}-retry, {token}-backoff\n"


def concurrently[T](calls: list[Callable[[], T]]) -> list[T]:
    """Run every call on its own thread, all released at once, and return the results in call order."""
    start = threading.Barrier(len(calls))

    def run(call: Callable[[], T]) -> T:
        start.wait()
        return call()

    with ThreadPoolExecutor(len(calls)) as pool:
        return list(pool.map(run, calls))


def test_concurrent_creates_all_succeed(create_with: CreateWith) -> None:
    # create_with asserts that every one of the ten responses is a 202.
    responses = concurrently([create_with] * 10)

    assert len({r.pattern().id for r in responses}) == 10, "ten concurrent creates must yield ten distinct ids"


def test_concurrent_updates_all_succeed(api: ApiClient, create_with: CreateWith) -> None:
    patterns = [create_with().pattern() for _ in range(10)]
    renamed = {p.id: f"{p.name}-renamed" for p in patterns}

    responses = concurrently(
        [partial(api.update, p.id, pattern_payload(renamed[p.id], "## Overview\n\nUpdated.\n")) for p in patterns]
    )

    assert [r.status for r in responses] == [204] * 10, [r.text for r in responses if r.status != 204]
    assert {p.id: api.get(p.id).pattern().name for p in patterns} == renamed


def test_concurrent_updates_to_one_pattern_all_succeed(
    api: ApiClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    tok = tokens.new()
    pattern = create_with().pattern()
    payloads = [pattern_payload(pattern.name, f"Updated content {i} {tok}", tags=[f"{tok}-{i}"]) for i in range(10)]

    responses = concurrently([partial(api.update, pattern.id, payload) for payload in payloads])

    assert [r.status for r in responses] == [204] * 10, [r.text for r in responses if r.status != 204]
    stored = api.get(pattern.id).json()
    # The last writer wins whole: content and tags come from the same request.
    assert (stored["content"], stored["tags"]) in [(p["content"], p["tags"]) for p in payloads], stored


def test_concurrent_related_patterns_get_one_edge_per_pair(
    api: ApiClient, graph: Graph, create_with: CreateWith, tokens: TokenPool
) -> None:
    content = related_content(tokens.new())
    ids = [r.pattern().id for r in concurrently([partial(create_with, content)] * 5)]
    for pattern_id in ids:
        wait.enriched(api, pattern_id)

    # related_pattern_ids matches RELATED_TO in either direction: one entry per edge.
    edges = {pattern_id: Counter(graph.related_pattern_ids(pattern_id)) for pattern_id in ids}
    for a, b in combinations(ids, 2):
        assert edges[a][b] == 1, f"{edges[a][b]} RELATED_TO edges between {a} and {b}; want exactly one"
