import json

import httpx
import pytest
from mcp import types
from mcp.shared.exceptions import MCPError

from mnemonic_e2e._http import TIMEOUT_S
from mnemonic_e2e.mcp import McpClient, ToolResult, text_of, tool_call_request


def test_tool_call_request_mirrors_method_and_name_into_headers() -> None:
    headers, body = tool_call_request("search_patterns", {"query": "x"})

    assert headers["Mcp-Method"] == "tools/call"
    assert headers["Mcp-Name"] == "search_patterns"
    assert headers["MCP-Protocol-Version"] == "2026-07-28"
    assert body["method"] == "tools/call"
    assert body["params"]["name"] == "search_patterns"
    assert body["params"]["arguments"] == {"query": "x"}
    assert "io.modelcontextprotocol/protocolVersion" in body["params"]["_meta"]


def test_post_sends_the_given_headers_and_body_to_the_mcp_path() -> None:
    seen: list[httpx.Request] = []

    def handle(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(405, text="no")

    mcp = McpClient("http://mcp.test:8081", transport=httpx.MockTransport(handle))
    response = mcp.post({"X-Test": "1"}, {"jsonrpc": "2.0"})

    assert response.status == 405
    assert response.text == "no"
    assert str(seen[0].url) == "http://mcp.test:8081/mcp"
    assert seen[0].headers["X-Test"] == "1"
    assert json.loads(seen[0].content) == {"jsonrpc": "2.0"}


def test_post_unreachable_error_names_the_url() -> None:
    def refuse(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("refused", request=request)

    mcp = McpClient("http://mcp.test:8081", transport=httpx.MockTransport(refuse))

    try:
        mcp.post({}, {})
    except ConnectionError as exc:
        assert "http://mcp.test:8081/mcp" in str(exc)
    else:
        raise AssertionError("expected ConnectionError")


def test_text_of_joins_text_blocks_and_carries_the_error_flag() -> None:
    result = types.CallToolResult(
        content=[
            types.TextContent(type="text", text="a"),
            types.TextContent(type="text", text="b"),
        ],
        is_error=True,
    )

    assert text_of(result) == ToolResult(text="a\nb", is_error=True)


class FakeSdkClient:
    """Stands in for mcp.Client: records its constructor call, returns or raises on call_tool."""

    created: list[tuple[str, dict[str, object]]] = []
    outcome: types.CallToolResult | Exception

    def __init__(self, url: str, **kwargs: object) -> None:
        FakeSdkClient.created.append((url, kwargs))

    async def __aenter__(self) -> "FakeSdkClient":
        return self

    async def __aexit__(self, *exc: object) -> None:
        return None

    async def call_tool(self, name: str, args: dict[str, object]) -> types.CallToolResult:
        if isinstance(self.outcome, Exception):
            raise self.outcome
        return self.outcome


@pytest.fixture
def fake_sdk(monkeypatch: pytest.MonkeyPatch) -> type[FakeSdkClient]:
    FakeSdkClient.created = []
    monkeypatch.setattr("mnemonic_e2e.mcp.Client", FakeSdkClient)
    return FakeSdkClient


def test_call_tool_returns_a_normal_result(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.outcome = types.CallToolResult(content=[types.TextContent(type="text", text="ok")], is_error=False)

    assert McpClient("http://mcp.test:8081").call_tool("t", {}) == ToolResult(text="ok", is_error=False)


def test_call_tool_keeps_a_tool_level_error_flag(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.outcome = types.CallToolResult(content=[types.TextContent(type="text", text="bad")], is_error=True)

    assert McpClient("http://mcp.test:8081").call_tool("t", {}) == ToolResult(text="bad", is_error=True)


def test_call_tool_turns_a_json_rpc_error_into_an_error_result(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.outcome = MCPError(-32602, "unknown tool: nope")

    assert McpClient("http://mcp.test:8081").call_tool("nope", {}) == ToolResult(
        text="unknown tool: nope", is_error=True
    )


def test_call_tool_gives_the_sdk_client_the_project_timeout(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.outcome = types.CallToolResult(content=[], is_error=False)

    McpClient("http://mcp.test:8081").call_tool("t", {})

    url, kwargs = fake_sdk.created[0]
    assert url == "http://mcp.test:8081/mcp"
    assert kwargs["read_timeout_seconds"] == TIMEOUT_S
