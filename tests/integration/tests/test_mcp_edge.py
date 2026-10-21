"""MCP edge cases over the real listener: pending and graph-less patterns, unknown ids, and the HTTP boundary.

These need the running compose stack. Every test makes its own patterns with
unique names, so the tests share one stack in any order.
"""

import uuid
from collections.abc import Callable, Iterator

import pytest

from mnemonic_e2e import wait
from mnemonic_e2e.api import ApiClient
from mnemonic_e2e.db import Graph, Pg
from mnemonic_e2e.mcp import PROTOCOL_VERSION, McpClient, json_rpc_message

# The MCP error text for an id with no pattern row (the server's "pattern not found" sentinel).
NOT_FOUND = "pattern not found"
FOREIGN_ORIGIN = {"Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}
# The two media types the streamable HTTP transport requires in Accept, for any protocol version.
TRANSPORT_HEADERS = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}

SeedPattern = Callable[[str], str]


@pytest.fixture
def seed_pattern(pg: Pg, api: ApiClient) -> Iterator[SeedPattern]:
    """Insert pending patterns straight into Postgres, with no enrichment job, and delete them when the test ends.

    With no job the enricher never touches the row: it stays pending and never gets a graph node,
    which an API-created pattern cannot guarantee (its job runs at once, and a forced embedding
    failure turns it failed, not pending). The API delete also clears any graph node.
    """
    seeded: list[str] = []

    def seed(name: str) -> str:
        pattern_id = pg.seed_pattern(name, content=f"Seeded pattern {name}.")
        seeded.append(pattern_id)
        return pattern_id

    yield seed

    for pattern_id in seeded:
        api.delete(pattern_id)


def tools_list_request() -> tuple[dict[str, str], dict[str, object]]:
    """A valid 2026-07-28 tools/list request, so a refusal can only come from what a test adds."""
    headers = {"MCP-Protocol-Version": PROTOCOL_VERSION, "Mcp-Method": "tools/list"}
    body = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/list",
        "params": {
            "_meta": {
                "io.modelcontextprotocol/protocolVersion": PROTOCOL_VERSION,
                "io.modelcontextprotocol/clientInfo": {"name": "mnemonic-e2e", "version": "0"},
                "io.modelcontextprotocol/clientCapabilities": {},
            }
        },
    }
    return headers, body


def test_get_pattern_pending_returns_markdown_with_pending(mcp: McpClient, seed_pattern: SeedPattern) -> None:
    name = wait.unique_name("integ-pending")
    pattern_id = seed_pattern(name)

    result = mcp.call_tool("get_pattern", {"id": pattern_id})

    assert not result.is_error, f"get_pattern on a pending pattern returned an error: {result.text}"
    for want in [f"## {name}", f"**ID:** {pattern_id}", "**Enrichment:** pending", f"Seeded pattern {name}."]:
        assert want in result.text, f"get_pattern is missing {want!r}:\n{result.text}"
    assert "### Extracted Concepts" not in result.text, f"a pending pattern shows graph context:\n{result.text}"


def test_find_related_without_graph_node_says_none(mcp: McpClient, graph: Graph, seed_pattern: SeedPattern) -> None:
    name = wait.unique_name("integ-nograph")
    pattern_id = seed_pattern(name)
    assert graph.pattern_count(pattern_id) == 0, "the seeded pattern already has a graph node"

    result = mcp.call_tool("find_related_patterns", {"pattern_id": pattern_id})

    assert not result.is_error, f"find_related_patterns returned an error: {result.text}"
    assert result.text == f"No related patterns found for '{name}'."


@pytest.mark.parametrize(("tool", "arg"), [("get_pattern", "id"), ("find_related_patterns", "pattern_id")])
def test_unknown_uuid_errors_for_both_tools(mcp: McpClient, tool: str, arg: str) -> None:
    unknown = str(uuid.uuid4())

    result = mcp.call_tool(tool, {arg: unknown})

    assert result.is_error, f"{tool} on an unknown id succeeded:\n{result.text}"
    # The text, not only the flag: a timeout or an unavailable database is an error result too.
    assert result.text.startswith(f"{NOT_FOUND}: "), f"{tool} on an unknown id: {result.text!r}"


def test_cross_site_post_is_403(mcp: McpClient) -> None:
    headers, body = tools_list_request()
    allowed = mcp.post(headers, body)
    assert allowed.status == 200, f"tools/list without an Origin: status {allowed.status}\nbody: {allowed.text}"

    refused = mcp.post({**headers, **FOREIGN_ORIGIN}, body)

    assert refused.status == 403, f"cross-site tools/list: status {refused.status}\nbody: {refused.text}"
    assert "cross-origin request detected" in refused.text


def test_get_mcp_is_405(mcp: McpClient) -> None:
    response = mcp.get({"Accept": "text/event-stream"})

    assert response.status == 405, f"GET /mcp: status {response.status}\nbody: {response.text}"
    assert response.headers.get("allow") == "POST"


def test_headerless_tools_call_works(mcp: McpClient) -> None:
    # A client older than 2026-07-28: no MCP-Protocol-Version, Mcp-Method or Mcp-Name, no _meta, no initialize.
    body = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {"name": "search_patterns", "arguments": {"query": "error handling"}},
    }

    response = mcp.post(TRANSPORT_HEADERS, body)

    assert response.status == 200, f"headerless tools/call: status {response.status}\nbody: {response.text}"
    message = json_rpc_message(response)
    assert "error" not in message, f"headerless tools/call returned a JSON-RPC error: {message}"
    result = message["result"]
    assert not result.get("isError"), f"headerless tools/call returned a tool error: {result}"
    assert [block["type"] for block in result["content"]] == ["text"]
    assert result["content"][0]["text"], "headerless tools/call returned empty text"
