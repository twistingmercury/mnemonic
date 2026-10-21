package mcpserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/twistingmercury/librarian/internal/mcp/config"
	"github.com/twistingmercury/librarian/internal/mcp/metrics"
	searchsvc "github.com/twistingmercury/librarian/internal/mcp/service/search"
)

const protocolVersion = "2026-07-28"

// newTestEndpoint serves the full /mcp stack (otelhttp, Origin check, SDK
// handler) over httptest and returns its URL.
func newTestEndpoint(t *testing.T) string {
	t.Helper()
	svc := Services{SearchPatterns: searchStub(t, nil, &searchsvc.SearchResult{}, nil)}
	cfg := config.MCPConfig{DefaultSearchThreshold: 0.5}
	srv := NewMCPHTTPServer(cfg, "localhost", NewMCPHTTPHandler(NewMCPServer(svc, zerolog.Nop(), cfg, noopToolMetrics(t))))
	ts := httptest.NewServer(srv.Handler)
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}

// noopToolMetrics returns tool instruments that record nothing.
func noopToolMetrics(t *testing.T) *metrics.Tools {
	t.Helper()
	m, err := metrics.NewTools(noop.NewMeterProvider().Meter("test"))
	require.NoError(t, err)
	return m
}

// modernMeta is the per-request _meta a 2026-07-28 client sends.
func modernMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    protocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "test", "version": "0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
}

// post sends one JSON-RPC request and returns the status and the decoded
// response, unwrapping SSE framing.
func post(t *testing.T, url string, headers map[string]string, body map[string]any) (int, map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		if d, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			raw = d
			break
		}
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// modern sends method as a 2026-07-28 request, with headers mirrored from the body.
func modern(t *testing.T, url, method, name string, params map[string]any) (int, map[string]any) {
	t.Helper()
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = modernMeta()
	headers := map[string]string{"MCP-Protocol-Version": protocolVersion, "Mcp-Method": method}
	if name != "" {
		params["name"] = name
		headers["Mcp-Name"] = name
	}
	return post(t, url, headers, map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
}

func TestServerDiscover(t *testing.T) {
	url := newTestEndpoint(t)
	status, resp := modern(t, url, "server/discover", "", nil)
	require.Equal(t, http.StatusOK, status)
	res := resp["result"].(map[string]any)
	assert.Equal(t, "complete", res["resultType"])
	assert.Contains(t, res["supportedVersions"], protocolVersion)
	assert.Equal(t, map[string]any{"tools": map[string]any{}}, res["capabilities"],
		"tools only: no deprecated logging, no listChanged for a fixed tool set")
	info := res["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	assert.Equal(t, "mnemonic", info["name"])
}

func TestToolsListCacheHint(t *testing.T) {
	url := newTestEndpoint(t)
	status, resp := modern(t, url, "tools/list", "", nil)
	require.Equal(t, http.StatusOK, status)
	res := resp["result"].(map[string]any)
	assert.Equal(t, float64(toolListTTL.Milliseconds()), res["ttlMs"])
	assert.Equal(t, "public", res["cacheScope"])
	var names []string
	for _, tool := range res["tools"].([]any) {
		names = append(names, tool.(map[string]any)["name"].(string))
	}
	assert.Equal(t, []string{"find_related_patterns", "get_pattern", "search_patterns"}, names, "deterministic order")
}

func TestToolsCallHeaderValidation(t *testing.T) {
	url := newTestEndpoint(t)
	status, resp := modern(t, url, "tools/call", "search_patterns", map[string]any{"arguments": map[string]any{"query": "x"}})
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "complete", resp["result"].(map[string]any)["resultType"])

	// Mcp-Name names a different tool than the body.
	status, resp = post(t, url,
		map[string]string{"MCP-Protocol-Version": protocolVersion, "Mcp-Method": "tools/call", "Mcp-Name": "get_pattern"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{
			"name": "search_patterns", "arguments": map[string]any{"query": "x"}, "_meta": modernMeta(),
		}})
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, float64(-32020), resp["error"].(map[string]any)["code"])
}

func TestOriginValidation(t *testing.T) {
	url := newTestEndpoint(t)
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list", "params": map[string]any{"_meta": modernMeta()}}
	headers := map[string]string{"MCP-Protocol-Version": protocolVersion, "Mcp-Method": "tools/list"}

	status, _ := post(t, url, headers, body)
	assert.Equal(t, http.StatusOK, status, "no Origin: non-browser client")

	headers["Origin"] = "https://evil.example"
	status, _ = post(t, url, headers, body)
	assert.Equal(t, http.StatusForbidden, status, "cross-origin browser request")
}

func TestLegacyClient(t *testing.T) {
	url := newTestEndpoint(t)
	status, resp := post(t, url, nil, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{
		"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "old", "version": "1"},
	}})
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "2025-11-25", resp["result"].(map[string]any)["protocolVersion"])

	status, resp = post(t, url, map[string]string{"MCP-Protocol-Version": "2025-11-25"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": map[string]any{
			"name": "search_patterns", "arguments": map[string]any{"query": "x"},
		}})
	require.Equal(t, http.StatusOK, status)
	assert.NotNil(t, resp["result"])
}

// TestGetMCPReturns405 verifies that the stateless handler offers no
// server-to-client stream: a GET on /mcp is refused rather than hanging open.
func TestGetMCPReturns405(t *testing.T) {
	svc := Services{SearchPatterns: searchStub(t, nil, &searchsvc.SearchResult{}, nil)}
	cfg := config.MCPConfig{DefaultSearchThreshold: 0.5}
	srv := NewMCPHTTPServer(cfg, "localhost", NewMCPHTTPHandler(NewMCPServer(svc, zerolog.Nop(), cfg, noopToolMetrics(t))))
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()

	srv.Handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, http.MethodPost, rec.Header().Get("Allow"), "tells the client POST is the only method")
}
