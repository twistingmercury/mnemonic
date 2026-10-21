# Authorization for the mnemonic-mcp edge (MCP Streamable HTTP on /mcp).
#
# Every MCP action is a JSON-RPC method in a POST body. Protocol 2026-07-28
# mirrors the method and tool name into the Mcp-Method and Mcp-Name headers,
# and the go-sdk server rejects a request whose headers disagree with its body,
# but only when MCP-Protocol-Version is 2026-07-28 or later. Below that version
# the headers are unchecked hints, so every POST must declare a supported
# version or it is denied. The SDK also rejects JSON-RPC batches for these
# versions, so one request is always one method.
package mnemonic.mcp

import rego.v1

import data.mnemonic.token

default allow := false

audience := "mnemonic-mcp"

# Add a version here only once the server validates headers for it.
supported_versions := {"2026-07-28"}

# Scope required for each tool. A tool missing from this map is denied.
tool_scopes := {
	"search_patterns": "mcp:search",
	"find_related_patterns": "mcp:read",
	"get_pattern": "mcp:read",
}

# Methods any authenticated caller may use.
open_methods := {"initialize", "ping", "tools/list"}

http := input.attributes.request.http

headers := http.headers

path := split(http.path, "?")[0]

mcp_method := headers["mcp-method"]

caller if {
	token.authenticated
	audience in token.audiences
}

modern if headers["mcp-protocol-version"] in supported_versions

allow if {
	http.method == "POST"
	path == "/mcp"
	caller
	modern
	post_allowed
}

# GET opens a server-to-client SSE stream and DELETE ends a session. Neither
# can invoke a tool.
allow if {
	http.method in {"GET", "DELETE"}
	path == "/mcp"
	caller
}

post_allowed if mcp_method in open_methods

post_allowed if startswith(mcp_method, "notifications/")

post_allowed if {
	mcp_method == "tools/call"
	tool_scopes[headers["mcp-name"]] in token.scopes
}

# Why a request was denied, for decision logs and tests.
reasons contains "missing or wrong-audience token" if not caller

reasons contains "unsupported or missing MCP-Protocol-Version" if {
	http.method == "POST"
	not modern
}

reasons contains "method not allowed" if {
	http.method == "POST"
	modern
	not post_allowed
}
