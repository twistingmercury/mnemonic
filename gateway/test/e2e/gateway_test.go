// Package e2e checks the gateway's allow/deny decisions against a running
// stack: Keycloak issues real tokens, Envoy verifies them and asks OPA.
//
// "Allowed" means the gateway forwarded the request, so any status other
// than 401 or 403 (neither service returns those itself). Run it against the
// stub stack (test/compose.stub.yaml) or the full one (docker-compose.yaml).
package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	apiURL      = env("GATEWAY_API_URL", "http://localhost:8080")
	mcpURL      = env("GATEWAY_MCP_URL", "http://localhost:8081")
	keycloakURL = env("KEYCLOAK_URL", "http://localhost:8180")
	client      = &http.Client{Timeout: 10 * time.Second}
)

const (
	mcpVersion = "2026-07-28"
	patternID  = "0b9a3c1e-8f7d-4c55-9a51-2f1f2a9e6b11"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// token gets an access token for one of the dev realm's service clients.
func token(t *testing.T, clientID string) string {
	t.Helper()
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientID + "-secret"},
	}
	resp, err := client.PostForm(keycloakURL+"/realms/mnemonic/protocol/openid-connect/token", form)
	if err != nil {
		t.Fatalf("token for %s: %v", clientID, err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.AccessToken == "" {
		t.Fatalf("token for %s: status %d, decode err %v", clientID, resp.StatusCode, err)
	}
	return body.AccessToken
}

type want int

const (
	allowed want = iota
	unauthenticated
	forbidden
	// denied is either 401 or 403, for cases where Envoy's choice between
	// them is not the point.
	denied
	// rejected is a 400 from Envoy for a request it refuses to parse.
	rejected
)

func (w want) String() string {
	return [...]string{"allowed", "401", "403", "401 or 403", "400"}[w]
}

func (w want) matches(status int) bool {
	switch w {
	case allowed:
		return status != http.StatusUnauthorized && status != http.StatusForbidden
	case unauthenticated:
		return status == http.StatusUnauthorized
	case forbidden:
		return status == http.StatusForbidden
	case rejected:
		return status == http.StatusBadRequest
	default:
		return status == http.StatusUnauthorized || status == http.StatusForbidden
	}
}

type tc struct {
	name    string
	client  string // Keycloak client to get a token for; "" sends none
	method  string
	url     string
	headers map[string]string
	body    string
	want    want
}

func run(t *testing.T, cases []tc) {
	tokens := map[string]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req, err := http.NewRequest(c.method, c.url, strings.NewReader(c.body))
			if err != nil {
				t.Fatal(err)
			}
			if c.client != "" {
				if _, ok := tokens[c.client]; !ok {
					tokens[c.client] = token(t, c.client)
				}
				req.Header.Set("Authorization", "Bearer "+tokens[c.client])
			}
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if !c.want.matches(resp.StatusCode) {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
				t.Fatalf("got %d, want %s: %s", resp.StatusCode, c.want, b)
			}
		})
	}
}

func TestAPIEdge(t *testing.T) {
	p := apiURL + "/v1/api/patterns"
	id := p + "/" + patternID
	run(t, []tc{
		{"version is public", "", "GET", apiURL + "/version", nil, "", allowed},
		{"health needs a token", "", "GET", apiURL + "/health", nil, "", unauthenticated},
		{"health with any api token", "api-reader", "GET", apiURL + "/health", nil, "", allowed},
		{"list without token", "", "GET", p, nil, "", unauthenticated},
		{"garbage token", "", "GET", p, map[string]string{"Authorization": "Bearer not.a.jwt"}, "", unauthenticated},
		{"reader lists", "api-reader", "GET", p + "?limit=5", nil, "", allowed},
		{"reader searches", "api-reader", "GET", p + "/search?q=errors", nil, "", allowed},
		{"reader gets one", "api-reader", "GET", id, nil, "", allowed},
		{"reader gets chunks", "api-reader", "GET", id + "/chunks", nil, "", allowed},
		{"reader cannot create", "api-reader", "POST", p, nil, "{}", forbidden},
		{"reader cannot update", "api-reader", "PUT", id, nil, "{}", forbidden},
		{"writer creates", "api-writer", "POST", p, nil, "{}", allowed},
		{"writer updates", "api-writer", "PUT", id, nil, "{}", allowed},
		{"writer reads", "api-writer", "GET", id, nil, "", allowed},
		{"writer cannot delete", "api-writer", "DELETE", id, nil, "", forbidden},
		{"admin deletes", "api-admin", "DELETE", id, nil, "", allowed},
		{"admin sees swagger", "api-admin", "GET", apiURL + "/swagger/index.html", nil, "", allowed},
		{"writer cannot see swagger", "api-writer", "GET", apiURL + "/swagger/index.html", nil, "", forbidden},
		{"mcp token rejected", "mcp-agent", "GET", p, nil, "", denied},
		{"unknown route", "api-admin", "GET", apiURL + "/v1/api/other", nil, "", forbidden},
		{"encoded slash rejected", "api-admin", "GET", p + "/a%2F..%2Fb", nil, "", rejected},
	})
}

func mcpHeaders(method, name string) map[string]string {
	h := map[string]string{
		"Content-Type":         "application/json",
		"Accept":               "application/json, text/event-stream",
		"MCP-Protocol-Version": mcpVersion,
		"Mcp-Method":           method,
	}
	if name != "" {
		h["Mcp-Name"] = name
	}
	return h
}

func toolCall(name string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":{"id":%q}}}`, name, patternID)
}

func TestMCPEdge(t *testing.T) {
	m := mcpURL + "/mcp"
	oldVersion := mcpHeaders("tools/call", "get_pattern")
	oldVersion["MCP-Protocol-Version"] = "2025-11-25"
	noHeaders := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
	run(t, []tc{
		{"no token", "", "POST", m, mcpHeaders("tools/list", ""), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, unauthenticated},
		{"tools/list", "mcp-limited", "POST", m, mcpHeaders("tools/list", ""), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, allowed},
		{"search with mcp:search", "mcp-agent", "POST", m, mcpHeaders("tools/call", "search_patterns"), toolCall("search_patterns"), allowed},
		{"search without mcp:search", "mcp-limited", "POST", m, mcpHeaders("tools/call", "search_patterns"), toolCall("search_patterns"), forbidden},
		{"get_pattern with mcp:read", "mcp-limited", "POST", m, mcpHeaders("tools/call", "get_pattern"), toolCall("get_pattern"), allowed},
		{"unknown tool", "mcp-agent", "POST", m, mcpHeaders("tools/call", "drop_everything"), toolCall("drop_everything"), forbidden},
		{"old protocol version", "mcp-agent", "POST", m, oldVersion, toolCall("get_pattern"), forbidden},
		{"no MCP headers", "mcp-agent", "POST", m, noHeaders, toolCall("get_pattern"), forbidden},
		{"api token rejected", "api-admin", "POST", m, mcpHeaders("tools/list", ""), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, denied},
		{"ops path not exposed", "mcp-agent", "GET", mcpURL + "/health", nil, "", forbidden},
	})
}

func TestMCPAuthDiscovery(t *testing.T) {
	resp, err := client.Post(mcpURL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	challenge := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, `resource_metadata="`) {
		t.Fatalf("got %d with WWW-Authenticate %q, want 401 with resource_metadata", resp.StatusCode, challenge)
	}

	resp, err = client.Get(mcpURL + "/.well-known/oauth-protected-resource/mcp")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var meta struct {
		Resource             string   `json:"resource"`
		AuthorizationServers []string `json:"authorization_servers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if meta.Resource != mcpURL+"/mcp" || len(meta.AuthorizationServers) == 0 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}

	// The advertised authorization server must publish its own metadata.
	resp, err = client.Get(meta.AuthorizationServers[0] + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorization server metadata: got %d", resp.StatusCode)
	}
}

// TestForwardedHeaders runs only against the stub upstreams, which echo the
// request they received.
func TestForwardedHeaders(t *testing.T) {
	req, _ := http.NewRequest("GET", apiURL+"/v1/api/patterns", nil)
	req.Header.Set("Authorization", "Bearer "+token(t, "api-reader"))
	req.Header.Set("X-Request-ID", "client-chosen")
	req.Header.Set("X-Correlation-ID", "client-chosen")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var echo struct {
		Stub    bool              `json:"stub"`
		Headers map[string]string `json:"headers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&echo); err != nil || !echo.Stub {
		t.Skip("upstream is not the stub")
	}
	if v, ok := echo.Headers["authorization"]; ok {
		t.Errorf("token forwarded to the service: %q", v)
	}
	if v := echo.Headers["x-request-id"]; v == "client-chosen" || v == "" {
		t.Errorf("x-request-id = %q, want one generated by Envoy", v)
	}
	if v, ok := echo.Headers["x-correlation-id"]; ok {
		t.Errorf("x-correlation-id forwarded: %q", v)
	}
}
