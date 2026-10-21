package mnemonic.api_test

import rego.v1

import data.envoy.authz

req(method, path, payload) := {"attributes": {
	"contextExtensions": {"edge": "api"},
	"request": {"http": {"method": method, "path": path, "headers": {}}},
	"metadataContext": {"filterMetadata": {"envoy.filters.http.jwt_authn": {"jwt_payload": payload}}},
}}

anon(method, path) := {"attributes": {
	"contextExtensions": {"edge": "api"},
	"request": {"http": {"method": method, "path": path, "headers": {}}},
}}

tok(scope) := {"sub": "svc", "aud": "mnemonic-api", "scope": scope}

reader := tok("patterns:read")

writer := tok("patterns:write")

admin := tok("patterns:admin")

id := "/v1/api/patterns/0b9a3c1e-8f7d-4c55-9a51-2f1f2a9e6b11"

test_version_is_public if authz.allow with input as anon("GET", "/version")

test_health_needs_token if {
	not authz.allow with input as anon("GET", "/health")
	authz.allow with input as req("GET", "/health", tok(""))
}

test_reader_can_read if {
	authz.allow with input as req("GET", "/v1/api/patterns", reader)
	authz.allow with input as req("GET", "/v1/api/patterns?limit=5&tags=go", reader)
	authz.allow with input as req("GET", "/v1/api/patterns/search?q=errors", reader)
	authz.allow with input as req("GET", id, reader)
	authz.allow with input as req("GET", concat("", [id, "/chunks"]), reader)
}

test_reader_cannot_write if {
	not authz.allow with input as req("POST", "/v1/api/patterns", reader)
	not authz.allow with input as req("PUT", id, reader)
	not authz.allow with input as req("DELETE", id, reader)
}

test_writer_can_create_and_update_but_not_delete if {
	authz.allow with input as req("GET", id, writer)
	authz.allow with input as req("POST", "/v1/api/patterns", writer)
	authz.allow with input as req("PUT", id, writer)
	not authz.allow with input as req("DELETE", id, writer)
}

test_admin_can_delete_and_see_swagger if {
	authz.allow with input as req("DELETE", id, admin)
	authz.allow with input as req("GET", "/swagger/index.html", admin)
	not authz.allow with input as req("GET", "/swagger/index.html", writer)
}

test_no_token_denied if {
	not authz.allow with input as anon("GET", "/v1/api/patterns")
	not authz.allow with input as anon("POST", "/v1/api/patterns")
}

test_mcp_audience_rejected if {
	not authz.allow with input as req("GET", "/v1/api/patterns", {"sub": "x", "aud": "mnemonic-mcp", "scope": "patterns:admin"})
}

test_audience_array_accepted if {
	authz.allow with input as req("GET", "/v1/api/patterns", {"sub": "x", "aud": ["account", "mnemonic-api"], "scope": "patterns:read"})
}

test_unknown_routes_denied if {
	not authz.allow with input as req("GET", "/v1/api/other", admin)
	not authz.allow with input as req("PUT", "/v1/api/patterns/search", admin)
	not authz.allow with input as req("DELETE", "/v1/api/patterns", admin)
	not authz.allow with input as req("GET", "/v1/api/patterns/x/chunks/y", admin)
	not authz.allow with input as req("PATCH", id, admin)
	not authz.allow with input as req("GET", "/metrics", admin)
}

test_untagged_edge_denied if {
	not authz.allow with input as {"attributes": {"request": {"http": {"method": "GET", "path": "/version", "headers": {}}}}}
}
