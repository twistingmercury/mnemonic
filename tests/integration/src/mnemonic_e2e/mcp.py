"""MCP client: tool calls through the official SDK, plus raw POSTs for edge cases."""

import asyncio
import json
from dataclasses import dataclass
from typing import Any

import httpx
from mcp import Client, types
from mcp.shared.exceptions import MCPError

from mnemonic_e2e._http import TIMEOUT_S, HttpResponse, new_client, send

PROTOCOL_VERSION = "2026-07-28"


@dataclass(frozen=True)
class ToolResult:
    text: str
    is_error: bool


def text_of(result: types.CallToolResult) -> ToolResult:
    """Join a tool result's text blocks and keep its error flag."""
    texts = [block.text for block in result.content if isinstance(block, types.TextContent)]
    return ToolResult(text="\n".join(texts), is_error=result.is_error)


def tool_call_request(name: str, args: dict[str, Any]) -> tuple[dict[str, str], dict[str, Any]]:
    """Build the headers and JSON-RPC body of a stateless tools/call request."""
    headers = {
        "MCP-Protocol-Version": PROTOCOL_VERSION,
        "Mcp-Method": "tools/call",
        "Mcp-Name": name,
    }
    body = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "tools/call",
        "params": {
            "name": name,
            "arguments": args,
            "_meta": {
                "io.modelcontextprotocol/protocolVersion": PROTOCOL_VERSION,
                "io.modelcontextprotocol/clientInfo": {
                    "name": "mnemonic-e2e",
                    "version": "0",
                },
                "io.modelcontextprotocol/clientCapabilities": {},
            },
        },
    }
    return headers, body


def json_rpc_message(response: HttpResponse) -> Any:
    """The JSON-RPC message of a raw MCP response, whether sent as plain JSON or as one SSE event."""
    if not response.headers.get("content-type", "").startswith("text/event-stream"):
        return response.json()

    for line in response.text.splitlines():
        if line.startswith("data:"):
            return json.loads(line.removeprefix("data:"))

    raise ValueError(f"SSE response has no data line: {response.text!r}")


def _root_cause(exc: BaseException) -> BaseException:
    """The first exception at the bottom of nested exception groups."""
    while isinstance(exc, BaseExceptionGroup):
        exc = exc.exceptions[0]
    return exc


class McpClient:
    def __init__(self, base_url: str, transport: httpx.BaseTransport | None = None) -> None:
        self._url = base_url.rstrip("/") + "/mcp"
        self._client = new_client(transport)

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        """Call a tool in its own SDK session and return its text and error flag."""
        return asyncio.run(self._call_tool(name, args))

    def call_ok(self, name: str, args: dict[str, Any]) -> str:
        """Call a tool that must succeed and return its text."""
        result = self.call_tool(name, args)
        assert not result.is_error, f"MCP {name}({args}) returned an error: {result.text}"
        return result.text

    async def _call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        # The SDK's anyio task groups wrap anything that leaves `async with Client` in an
        # ExceptionGroup, so a JSON-RPC error is caught inside the block, and a group that
        # escapes it is a failure to reach or talk to the server.
        try:
            async with Client(self._url, read_timeout_seconds=TIMEOUT_S) as client:
                try:
                    return text_of(await client.call_tool(name, args))
                except MCPError as exc:
                    # A JSON-RPC error (unknown tool, bad arguments) is a result, as a tool-level error is.
                    return ToolResult(text=exc.message, is_error=True)
        except ExceptionGroup as group:
            raise ConnectionError(f"MCP tools/call {name} at {self._url}: {_root_cause(group)!r}") from group

    def post(self, headers: dict[str, str], body: Any) -> HttpResponse:
        """POST body as JSON with exactly these headers, for malformed-request tests."""
        merged = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            **headers,
        }
        return send(self._client, "POST", self._url, headers=merged, json=body)

    def get(self, headers: dict[str, str]) -> HttpResponse:
        """GET the MCP endpoint with exactly these headers."""
        return send(self._client, "GET", self._url, headers=headers)
