"""MCP client: tool calls through the official SDK, plus raw POSTs for edge cases."""

import asyncio
from dataclasses import dataclass
from typing import Any

import httpx
from mcp import Client, types

from mnemonic_e2e._http import HttpResponse, new_client, send

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


class McpClient:
    def __init__(self, base_url: str, transport: httpx.BaseTransport | None = None) -> None:
        self._url = base_url.rstrip("/") + "/mcp"
        self._client = new_client(transport)

    def call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        """Call a tool in its own SDK session and return its text and error flag."""
        return asyncio.run(self._call_tool(name, args))

    async def _call_tool(self, name: str, args: dict[str, Any]) -> ToolResult:
        try:
            async with Client(self._url) as client:
                return text_of(await client.call_tool(name, args))
        except httpx.TransportError as exc:
            raise ConnectionError(f"MCP tools/call {name} at {self._url}: {exc}") from exc

    def post(self, headers: dict[str, str], body: Any) -> HttpResponse:
        """POST body as JSON with exactly these headers, for malformed-request tests."""
        merged = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            **headers,
        }
        return send(self._client, "POST", self._url, headers=merged, json=body)
