# Entry point for Envoy's ext_authz check. Each Envoy route tags its requests
# with a context extension named "edge", and the decision is delegated to the
# policy for that edge. Anything untagged is denied.
package envoy.authz

import rego.v1

import data.mnemonic.api
import data.mnemonic.mcp

default allow := false

edge := input.attributes.contextExtensions.edge

allow if {
	edge == "api"
	api.allow
}

allow if {
	edge == "mcp"
	mcp.allow
}
