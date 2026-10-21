package mnemonic.mcp_test

import rego.v1

import data.envoy.authz
import data.mnemonic.mcp

v := "2026-07-28"

post(hdrs, payload) := {"attributes": {
	"contextExtensions": {"edge": "mcp"},
	"request": {"http": {"method": "POST", "path": "/mcp", "headers": hdrs}},
	"metadataContext": {"filterMetadata": {"envoy.filters.http.jwt_authn": {"jwt_payload": payload}}},
}}

call(tool) := {"mcp-protocol-version": v, "mcp-method": "tools/call", "mcp-name": tool}

tok(scope) := {"sub": "agent", "aud": "mnemonic-mcp", "scope": scope}

full := tok("mcp:read mcp:search")

read_only := tok("mcp:read")

test_handshake_and_listing if {
	authz.allow with input as post({"mcp-protocol-version": v, "mcp-method": "initialize"}, tok(""))
	authz.allow with input as post({"mcp-protocol-version": v, "mcp-method": "tools/list"}, tok(""))
	authz.allow with input as post({"mcp-protocol-version": v, "mcp-method": "notifications/initialized"}, tok(""))
}

test_tool_scopes if {
	authz.allow with input as post(call("search_patterns"), full)
	authz.allow with input as post(call("get_pattern"), read_only)
	authz.allow with input as post(call("find_related_patterns"), read_only)
	not authz.allow with input as post(call("search_patterns"), read_only)
}

test_unknown_tool_denied if {
	not authz.allow with input as post(call("drop_everything"), full)
	not authz.allow with input as post({"mcp-protocol-version": v, "mcp-method": "tools/call"}, full)
}

test_other_methods_denied if {
	not authz.allow with input as post({"mcp-protocol-version": v, "mcp-method": "resources/list"}, full)
	not authz.allow with input as post({"mcp-protocol-version": v}, full)
}

# The bypass: below 2026-07-28 the server never checks headers against the
# body, so a harmless-looking header could carry a tools/call body.
test_old_or_missing_version_denied if {
	not authz.allow with input as post({"mcp-method": "tools/list"}, full)
	not authz.allow with input as post({"mcp-protocol-version": "2025-11-25", "mcp-method": "initialize"}, full)
	not authz.allow with input as post({"mcp-protocol-version": "2025-11-25", "mcp-method": "tools/call", "mcp-name": "get_pattern"}, full)
	"unsupported or missing MCP-Protocol-Version" in mcp.reasons with input as post({"mcp-method": "tools/list"}, full)
}

test_api_audience_rejected if {
	not authz.allow with input as post(call("get_pattern"), {"sub": "x", "aud": "mnemonic-api", "scope": "mcp:read"})
}

test_no_token_denied if {
	not authz.allow with input as {"attributes": {
		"contextExtensions": {"edge": "mcp"},
		"request": {"http": {"method": "POST", "path": "/mcp", "headers": call("get_pattern")}},
	}}
}

test_stream_and_session_end if {
	authz.allow with input as {"attributes": {
		"contextExtensions": {"edge": "mcp"},
		"request": {"http": {"method": "GET", "path": "/mcp", "headers": {}}},
		"metadataContext": {"filterMetadata": {"envoy.filters.http.jwt_authn": {"jwt_payload": tok("")}}},
	}}
}

test_other_paths_denied if {
	not authz.allow with input as {"attributes": {
		"contextExtensions": {"edge": "mcp"},
		"request": {"http": {"method": "POST", "path": "/health", "headers": call("get_pattern")}},
		"metadataContext": {"filterMetadata": {"envoy.filters.http.jwt_authn": {"jwt_payload": full}}},
	}}
}
