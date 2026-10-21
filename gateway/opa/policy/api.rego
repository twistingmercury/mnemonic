# Authorization for the mnemonic-api edge (REST, Gin router).
# Scopes are hierarchical: patterns:admin includes patterns:write, which
# includes patterns:read.
package mnemonic.api

import rego.v1

import data.mnemonic.token

default allow := false

audience := "mnemonic-api"

http := input.attributes.request.http

# Envoy's path includes the query string.
path := split(http.path, "?")[0]

can_read if {
	some s in {"patterns:read", "patterns:write", "patterns:admin"}
	s in token.scopes
}

can_write if {
	some s in {"patterns:write", "patterns:admin"}
	s in token.scopes
}

can_admin if "patterns:admin" in token.scopes

caller if {
	token.authenticated
	audience in token.audiences
}

# Build info is public.
allow if {
	http.method == "GET"
	path == "/version"
}

# The heartbeat names every dependency and its state, so it needs a token.
allow if {
	http.method == "GET"
	path == "/health"
	caller
}

allow if {
	http.method == "GET"
	startswith(path, "/swagger/")
	caller
	can_admin
}

allow if {
	http.method == "GET"
	pattern_read_path
	caller
	can_read
}

allow if {
	http.method == "POST"
	path == "/v1/api/patterns"
	caller
	can_write
}

allow if {
	http.method == "PUT"
	pattern_id_path
	caller
	can_write
}

allow if {
	http.method == "DELETE"
	pattern_id_path
	caller
	can_admin
}

pattern_read_path if path == "/v1/api/patterns"

pattern_read_path if path == "/v1/api/patterns/search"

pattern_read_path if pattern_id_path

pattern_read_path if {
	parts := split(path, "/")
	count(parts) == 6
	parts[3] == "patterns"
	parts[4] != ""
	parts[5] == "chunks"
	prefix_ok(parts)
}

pattern_id_path if {
	parts := split(path, "/")
	count(parts) == 5
	prefix_ok(parts)
	parts[3] == "patterns"
	not parts[4] in {"", "search"}
}

# parts of "/v1/api/patterns/..." are ["", "v1", "api", "patterns", ...]
prefix_ok(parts) if {
	parts[0] == ""
	parts[1] == "v1"
	parts[2] == "api"
}
