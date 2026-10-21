# Helpers over the JWT that Envoy's jwt_authn filter has already verified
# (signature, issuer, expiry, audience). Envoy passes the verified payload to
# OPA as filter metadata; the raw token never reaches OPA.
package mnemonic.token

import rego.v1

claims := input.attributes.metadataContext.filterMetadata["envoy.filters.http.jwt_authn"].jwt_payload

authenticated if claims.sub

scopes contains s if {
	some s in split(object.get(claims, "scope", ""), " ")
	s != ""
}

# aud may be a string or an array.
audiences contains claims.aud if is_string(claims.aud)

audiences contains a if {
	is_array(claims.aud)
	some a in claims.aud
}
