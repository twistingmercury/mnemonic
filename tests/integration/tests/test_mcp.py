import json

import httpx
import pytest
from mcp import types
from mcp.shared.exceptions import MCPError

from mnemonic_e2e._http import TIMEOUT_S, HttpResponse
from mnemonic_e2e.mcp import McpClient, ToolResult, json_rpc_message, text_of, tool_call_request


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


def task_group_error(exc: Exception) -> ExceptionGroup[Exception]:
    """Wrap exc the way the SDK's nested anyio task groups do (mcp 2.3.0: two levels)."""
    return ExceptionGroup("unhandled errors in a TaskGroup", [ExceptionGroup("unhandled errors in a TaskGroup", [exc])])


class FakeSdkClient:
    """Stands in for mcp.Client: records its constructor call, returns or raises on call_tool.

    Like the real client, it fails to connect with an exception group from
    __aenter__, and wraps whatever leaves the async with block in one.
    """

    created: list[tuple[str, dict[str, object]]] = []
    outcome: types.CallToolResult | Exception
    connect_error: Exception | None = None

    def __init__(self, url: str, **kwargs: object) -> None:
        FakeSdkClient.created.append((url, kwargs))

    async def __aenter__(self) -> "FakeSdkClient":
        if self.connect_error is not None:
            raise task_group_error(self.connect_error)

        return self

    async def __aexit__(self, exc_type: object, exc: BaseException | None, tb: object) -> None:
        if isinstance(exc, Exception):
            raise task_group_error(exc) from None

    async def call_tool(self, name: str, args: dict[str, object]) -> types.CallToolResult:
        if isinstance(self.outcome, Exception):
            raise self.outcome
        return self.outcome


@pytest.fixture
def fake_sdk(monkeypatch: pytest.MonkeyPatch) -> type[FakeSdkClient]:
    FakeSdkClient.created = []
    FakeSdkClient.connect_error = None
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


def test_call_tool_unreachable_error_names_the_url_and_the_cause(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.connect_error = OSError("Name or service not known")

    with pytest.raises(ConnectionError, match="http://mcp.test:8081/mcp") as info:
        McpClient("http://mcp.test:8081").call_tool("t", {})

    assert "Name or service not known" in str(info.value)


def test_get_sends_a_get_with_the_given_headers_to_the_mcp_path() -> None:
    seen: list[httpx.Request] = []

    def handle(request: httpx.Request) -> httpx.Response:
        seen.append(request)
        return httpx.Response(405, headers={"Allow": "POST"})

    response = McpClient("http://mcp.test:8081", transport=httpx.MockTransport(handle)).get({"Accept": "x"})

    assert response.status == 405
    assert response.headers["allow"] == "POST"
    assert seen[0].method == "GET"
    assert str(seen[0].url) == "http://mcp.test:8081/mcp"
    assert seen[0].headers["Accept"] == "x"


def test_json_rpc_message_reads_a_plain_json_body() -> None:
    response = HttpResponse(200, {"content-type": "application/json"}, b'{"jsonrpc":"2.0","id":1,"result":{}}')

    assert json_rpc_message(response) == {"jsonrpc": "2.0", "id": 1, "result": {}}


def test_json_rpc_message_reads_the_data_line_of_an_sse_body() -> None:
    body = b'event: message\nid: 1\ndata: {"jsonrpc":"2.0","id":1,"result":{"isError":false}}\n\n'
    response = HttpResponse(200, {"content-type": "text/event-stream"}, body)

    assert json_rpc_message(response) == {"jsonrpc": "2.0", "id": 1, "result": {"isError": False}}


def test_json_rpc_message_rejects_an_sse_body_without_data() -> None:
    response = HttpResponse(200, {"content-type": "text/event-stream"}, b"event: message\n\n")

    with pytest.raises(ValueError, match="no data line"):
        json_rpc_message(response)


def test_call_ok_returns_the_text_of_a_successful_call(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.outcome = types.CallToolResult(content=[types.TextContent(type="text", text="fine")], is_error=False)

    assert McpClient("http://mcp.test:8081").call_ok("get_pattern", {}) == "fine"


def test_call_ok_fails_with_the_error_text_of_a_failed_call(fake_sdk: type[FakeSdkClient]) -> None:
    fake_sdk.outcome = types.CallToolResult(
        content=[types.TextContent(type="text", text="no such pattern")], is_error=True
    )

    with pytest.raises(AssertionError, match="no such pattern"):
        McpClient("http://mcp.test:8081").call_ok("get_pattern", {})
