"""Full-stack flows: a pattern written through the API, enriched over RabbitMQ, read back through MCP.

These need the running compose stack. Every test makes its own patterns with
unique names and search tokens, so the tests share one stack in any order.
"""

from collections.abc import Callable

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.api import ApiClient, Pattern, pattern_payload
from mnemonic_e2e.config import Settings
from mnemonic_e2e.mcp import McpClient
from mnemonic_e2e.stub import StubClient
from mnemonic_e2e.tokens import TokenPool

# The enricher's retry sweep runs every 15s: one full interval plus enrichment.
RETRY_WINDOW_S = 45.0
# The stub scores a one-word query against a whole chunk at about 0.17; unrelated text scores 0.
SEARCH_THRESHOLD = 0.1

# The create_pattern fixture (conftest.py): name, content -> the created pattern.
CreatePattern = Callable[[str, str], Pattern]


def pattern_content(words: str = "", *concepts: str) -> str:
    """Enrichable content: one decorated section the API turns into a chunk, plus a concept line for the stub."""
    return f"""## Overview

Integration test pattern.

Concepts: {", ".join(concepts)}

[//]: pattern

## Core Approach

Wrap errors with fmt.Errorf and the %w verb, then inspect them with errors.As. {words}
"""


@pytest.fixture(scope="module")
def tokens() -> TokenPool:
    # Every word the test patterns have in common, names and tags included.
    shared = pattern_content("", "errors", "wrapping", "unrelated") + " integ a b c integration-test"
    return TokenPool(shared)


def _fetch(api: ApiClient, pattern_id: str) -> Pattern:
    response = api.get(pattern_id)
    assert response.status == 200, f"GET pattern {pattern_id}: status {response.status}\nbody: {response.text}"
    return response.pattern()


def api_search(api: ApiClient, query: str) -> list[str]:
    response = api.search(query, threshold=SEARCH_THRESHOLD)
    assert response.status == 200, f"API search {query!r}: status {response.status}\nbody: {response.text}"
    return response.pattern_ids()


def mcp_search(mcp: McpClient, query: str) -> str:
    return mcp.call_ok("search_patterns", {"query": query, "threshold": SEARCH_THRESHOLD})


@pytest.mark.parametrize(
    ("service", "deps"),
    [
        ("api", {"PostgreSQL", "Neo4j", "OpenAI embedding model", "RabbitMQ"}),
        ("mcp", {"PostgreSQL", "Neo4j", "OpenAI embedding model"}),
        ("enricher", {"PostgreSQL", "Neo4j", "OpenAI embedding model", "OpenAI extraction model"}),
    ],
    ids=["api", "mcp", "enricher"],
)
def test_services_healthy(settings: Settings, service: str, deps: set[str]) -> None:
    url = {"api": settings.api_url, "mcp": settings.mcp_ops_url, "enricher": settings.enricher_url}[service]
    client = ApiClient(url)

    health = client.get_path("/health")
    assert health.status == 200, f"{url}/health: status {health.status}\nbody: {health.text}"
    body = health.json()
    not_ok = {d["name"]: f"{d['status']}: {d.get('message', '')}" for d in body["dependencies"] if d["status"] != "OK"}
    assert not_ok == {}
    assert sorted(d["name"] for d in body["dependencies"]) == sorted(deps)
    assert body["status"] == "OK"

    version = client.get_path("/version")
    assert version.status == 200, f"{url}/version: status {version.status}\nbody: {version.text}"
    assert version.json()["version"] != ""


def test_pattern_flows_from_api_to_mcp(
    api: ApiClient, mcp: McpClient, tokens: TokenPool, create_pattern: CreatePattern
) -> None:
    tok = tokens.new()
    concepts = [f"{tok}-errors", f"{tok}-wrapping"]
    p = create_pattern(f"integ-{tok}", pattern_content("", *concepts))
    assert p.enrichment_status == "pending"

    enriched = wait.enriched(api, p.id)
    assert enriched.enriched_at, "enriched_at is empty after enrichment"

    chunks = api.chunks(p.id)
    assert chunks.status == 200, f"GET chunks: status {chunks.status}\nbody: {chunks.text}"
    assert [c.enrichment_status for c in chunks.chunk_list()] == ["enriched"]

    assert p.id in api_search(api, tok)
    assert p.name in mcp_search(mcp, tok)

    text = mcp.call_ok("get_pattern", {"id": p.id})
    for want in [p.name, "**Enrichment:** enriched", "### Extracted Concepts", *concepts]:
        assert want in text, f"MCP get_pattern is missing {want!r}:\n{text}"


def test_update_reenriches_pattern(
    api: ApiClient, mcp: McpClient, tokens: TokenPool, create_pattern: CreatePattern
) -> None:
    old_word, new_word = tokens.new(), tokens.new()
    p = create_pattern(f"integ-{tokens.new()}", pattern_content(old_word))
    wait.enriched(api, p.id)

    updated = api.update(p.id, pattern_payload(p.name, pattern_content(new_word)))
    assert updated.status == 204, f"update: status {updated.status}\nbody: {updated.text}"
    wait.enriched(api, p.id)

    assert p.id in api_search(api, new_word)
    assert p.id not in api_search(api, old_word)
    assert p.name in mcp_search(mcp, new_word)
    assert p.name not in mcp_search(mcp, old_word)


def test_related_patterns_and_delete(
    api: ApiClient, mcp: McpClient, tokens: TokenPool, create_pattern: CreatePattern
) -> None:
    tok = tokens.new()
    shared = [f"{tok}-retry", f"{tok}-backoff", f"{tok}-jitter"]

    # Enriched one after the other: the enricher links a pattern to those already in the graph when it syncs.
    a = create_pattern(f"integ-a-{tok}", pattern_content("", *shared))
    wait.enriched(api, a.id)
    b = create_pattern(f"integ-b-{tok}", pattern_content("", *shared))
    unrelated = create_pattern(f"integ-c-{tok}", pattern_content("", f"{tok}-unrelated"))
    wait.enriched(api, b.id)
    wait.enriched(api, unrelated.id)

    text = mcp.call_ok("find_related_patterns", {"pattern_id": a.id})
    assert b.name in text
    assert unrelated.name not in text, f"find_related_patterns(a) lists a pattern with no shared concepts:\n{text}"
    for concept in shared:
        assert concept in text, f"find_related_patterns(a) is missing shared concept {concept!r}:\n{text}"

    deleted = api.delete(a.id)
    assert deleted.status == 204, f"delete: status {deleted.status}\nbody: {deleted.text}"
    assert api.get(a.id).status == 404

    gone = mcp.call_tool("get_pattern", {"id": a.id})
    assert gone.is_error, f"MCP get_pattern still returns the deleted pattern:\n{gone.text}"
    assert a.name not in mcp_search(mcp, tok)
    assert a.name not in mcp.call_ok("find_related_patterns", {"pattern_id": b.id})


def test_enricher_retries_failed_job(
    api: ApiClient, injected_failures: StubClient, tokens: TokenPool, create_pattern: CreatePattern
) -> None:
    # Compose runs the enricher with no in-call OpenAI retries, so the forced failure reaches the job.
    tok = tokens.new()
    injected_failures.fail(match=tok, n=1)

    p = create_pattern(f"integ-{tok}", pattern_content())

    statuses: list[str] = []

    def status() -> str:
        current = _fetch(api, p.id)
        statuses.append(current.enrichment_status)
        return current.enrichment_status

    wait.until(
        status, f"pattern {p.id} to recover through the retry sweep", RETRY_WINDOW_S, done=lambda s: s == "enriched"
    )

    assert injected_failures.remaining(tok) == 0, "the forced failure was never served"
    assert "failed" in statuses, f"never saw the pattern fail, so the retry path was not exercised: {statuses}"
