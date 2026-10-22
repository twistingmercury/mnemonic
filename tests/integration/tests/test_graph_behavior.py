"""Graph behavior seen through the API and MCP: related patterns, concepts, delete and update pruning.

These need the running compose stack. The OpenAI stub hands the enricher the
concepts listed on a "Concepts:" line of the content, so each test controls
exactly which concepts its patterns share. Concept names carry the test's unique
token, so a test's graph assertions never see another test's concepts.
"""

from collections.abc import Callable
from typing import Any

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.api import ApiClient, ApiResponse, Pattern, pattern_payload
from mnemonic_e2e.db import Graph
from mnemonic_e2e.mcp import McpClient
from mnemonic_e2e.tokens import TokenPool

# The create_with fixture (conftest.py): payload overrides -> the create response.
CreateWith = Callable[..., ApiResponse]
# The enriched fixture: concept names -> the created, enriched pattern.
Enriched = Callable[..., Pattern]


def graph_content(*concepts: str) -> str:
    """Single-chunk content whose stub-extracted concepts are exactly the ones given."""
    return f"## Overview\n\nGraph test pattern.\n\nConcepts: {', '.join(concepts)}\n"


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    # Every word the test patterns have in common, tags and names included.
    return TokenPool(graph_content() + " integ api")


def graph_context(api: ApiClient, pattern_id: str) -> dict[str, Any]:
    """The graph context (related patterns and concepts) of GET /patterns/{id}."""
    graph: dict[str, Any] = api.get(pattern_id).json()["graph"]
    return graph


def related_ids(api: ApiClient, pattern_id: str) -> list[str]:
    """The ids GET /patterns/{id} lists as related, in response order."""
    return [r["id"] for r in graph_context(api, pattern_id)["related_patterns"]]


def positions(text: str, names: list[str], marker: str) -> list[int]:
    """Where each name appears in text as marker + name, so a test can assert their order."""
    return [text.index(marker + name) for name in names]


@pytest.fixture
def enriched(api: ApiClient, create_with: CreateWith) -> Enriched:
    """Create a pattern whose concepts are exactly the ones given and return it once enriched."""

    def create(*concepts: str) -> Pattern:
        pattern = create_with(graph_content(*concepts)).pattern()
        wait.enriched(api, pattern.id)
        return pattern

    return create


def test_related_patterns_are_ordered_by_similarity_and_limited(
    api: ApiClient, mcp: McpClient, enriched: Enriched, tokens: TokenPool
) -> None:
    tok = tokens.new()
    a, b, c = f"{tok}-a", f"{tok}-b", f"{tok}-c"
    source = enriched(a, b, c)
    # Similarity is the shared concepts over the larger concept count: 1/3, 1 and 2/3.
    # Created weakest first, then strongest, then middle, so insertion order is not the expected order.
    one = enriched(a)
    three = enriched(a, b, c)
    two = enriched(a, b)

    related = graph_context(api, source.id)["related_patterns"]
    assert [r["id"] for r in related] == [three.id, two.id, one.id]
    assert [r["strength"] for r in related] == sorted((r["strength"] for r in related), reverse=True)

    text = mcp.call_ok("find_related_patterns", {"pattern_id": source.id})
    where = positions(text, [three.name, two.name, one.name], "## ")
    assert where == sorted(where), f"find_related_patterns is not ordered by similarity:\n{text}"

    limited = mcp.call_ok("find_related_patterns", {"pattern_id": source.id, "limit": 2})
    assert three.name in limited
    assert two.name in limited
    assert one.name not in limited, f"limit 2 still lists the weakest pattern:\n{limited}"


def test_graph_context_lists_at_most_ten_related_patterns(
    api: ApiClient, create_with: CreateWith, tokens: TokenPool
) -> None:
    body = graph_content(f"{tokens.new()}-shared")
    ids = [create_with(body).pattern().id for _ in range(12)]
    for pattern_id in ids:
        wait.enriched(api, pattern_id)

    # Each of the twelve is related to the eleven others.
    assert len(related_ids(api, ids[0])) == 10


def test_source_is_not_related_to_itself(
    api: ApiClient, mcp: McpClient, graph: Graph, enriched: Enriched, tokens: TokenPool
) -> None:
    shared = f"{tokens.new()}-shared"
    source = enriched(shared)
    other = enriched(shared)

    assert related_ids(api, source.id) == [other.id]
    assert graph.related_pattern_ids(source.id) == [other.id]
    text = mcp.call_ok("find_related_patterns", {"pattern_id": source.id})
    assert f"## {other.name} (" in text
    assert f"## {source.name} (" not in text, f"the source is listed as related to itself:\n{text}"


def test_concepts_are_ordered_by_name(api: ApiClient, mcp: McpClient, enriched: Enriched, tokens: TokenPool) -> None:
    tok = tokens.new()
    pattern = enriched(f"{tok}-zulu", f"{tok}-mike", f"{tok}-alpha")
    ordered = [f"{tok}-alpha", f"{tok}-mike", f"{tok}-zulu"]

    assert [c["name"] for c in graph_context(api, pattern.id)["concepts"]] == ordered

    text = mcp.call_ok("get_pattern", {"id": pattern.id})
    where = positions(text, ordered, "- **")
    assert where == sorted(where), f"get_pattern does not list concepts by name:\n{text}"


def test_delete_removes_only_orphaned_concepts(
    api: ApiClient, graph: Graph, enriched: Enriched, tokens: TokenPool
) -> None:
    tok = tokens.new()
    shared, own_1, own_2 = f"{tok}-shared", f"{tok}-own1", f"{tok}-own2"
    first = enriched(shared, own_1)
    second = enriched(shared, own_2)
    assert graph.concepts_with_prefix(tok) == [own_1, own_2, shared]

    assert api.delete(first.id).status == 204

    assert graph.concepts_with_prefix(tok) == [own_2, shared], "delete must drop only the concept nobody else mentions"
    assert graph.concepts_of(second.id) == [own_2, shared]


def test_update_prunes_stale_concepts_and_edges(
    api: ApiClient, graph: Graph, enriched: Enriched, tokens: TokenPool
) -> None:
    tok = tokens.new()
    a, b, c, x = f"{tok}-a", f"{tok}-b", f"{tok}-c", f"{tok}-x"
    pattern = enriched(a, b, c)
    peer = enriched(a, b, c)
    assert graph.concepts_of(pattern.id) == [a, b, c]
    assert graph.related_pattern_ids(pattern.id) == [peer.id]

    updated = api.update(pattern.id, pattern_payload(pattern.name, graph_content(x)))
    assert updated.status == 204, updated.text
    wait.enriched(api, pattern.id)

    assert graph.concepts_of(pattern.id) == [x], "the dropped concepts are still mentioned in the updated pattern"
    assert graph.related_pattern_ids(pattern.id) == [], (
        "the edge to a pattern it no longer shares concepts with remains"
    )
    assert graph.concepts_of(peer.id) == [a, b, c], "the update must not touch the peer's concepts"
