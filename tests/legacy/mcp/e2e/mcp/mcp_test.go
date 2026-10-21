// Package mcp_test provides end-to-end tests for the Mnemonic MCP server.
//
// These tests validate the MCP JSON-RPC interface from an external consumer's
// perspective, treating the server as a black box. All requests go through the
// network using the JSON-RPC 2.0 protocol over HTTP POST /mcp.
package mcp_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/twistingmercury/librarian-mcp/tests/e2e/helpers"
)

// Section 1: tools/list

func TestMCPToolsList_ReturnsThreeTools(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.ListTools(t)
	if resp.Error != nil {
		t.Fatalf("ListTools returned error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("ListTools returned nil result")
	}
	tools := resp.Result.Tools
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"search_patterns", "find_related_patterns", "get_pattern"} {
		if !names[expected] {
			t.Errorf("expected tool %q not found in list", expected)
		}
	}
}

func TestMCPToolsList_ToolsHaveDescriptions(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.ListTools(t)
	if resp.Error != nil {
		t.Fatalf("ListTools returned error: %v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("ListTools returned nil result")
	}
	for _, tool := range resp.Result.Tools {
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}
}

// Section 2: get_pattern validation

func TestMCPGetPattern_EmptyID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "get_pattern", map[string]any{"id": ""})
	helpers.AssertMCPError(t, resp)
}

func TestMCPGetPattern_InvalidUUID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "get_pattern", map[string]any{"id": "not-a-uuid"})
	helpers.AssertMCPError(t, resp)
}

// Section 3: get_pattern data paths

func TestMCPGetPattern_ExistingPattern_ReturnsPendingMarkdown(t *testing.T) {
	apiClient := helpers.NewTestClient(t)
	pattern := helpers.CreateTestPattern(t, apiClient)

	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "get_pattern", map[string]any{"id": pattern.ID})
	result := helpers.AssertMCPSuccess(t, resp)

	if len(result.Content) == 0 {
		t.Fatal("expected content in result, got none")
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected content type 'text', got %q", result.Content[0].Type)
	}
	text := result.Content[0].Text
	if !strings.Contains(text, pattern.Name) {
		t.Errorf("expected result to contain pattern name %q, got: %s", pattern.Name, text)
	}
	if !strings.Contains(strings.ToLower(text), "pending") {
		t.Errorf("expected result to contain 'pending' status, got: %s", text)
	}
}

func TestMCPGetPattern_NonExistentID_ReturnsNotFoundError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "get_pattern", map[string]any{"id": uuid.New().String()})
	helpers.AssertMCPError(t, resp)
}

// Section 4: find_related_patterns validation

func TestMCPFindRelated_EmptyPatternID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": ""})
	helpers.AssertMCPError(t, resp)
}

func TestMCPFindRelated_InvalidUUID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": "bad-uuid"})
	helpers.AssertMCPError(t, resp)
}

func TestMCPFindRelated_LimitTooLow_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{
		"pattern_id": uuid.New().String(),
		"limit":      0,
	})
	helpers.AssertMCPError(t, resp)
}

func TestMCPFindRelated_LimitTooHigh_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{
		"pattern_id": uuid.New().String(),
		"limit":      21,
	})
	helpers.AssertMCPError(t, resp)
}

// Section 5: find_related_patterns data paths

// TestMCPFindRelated_UnenrichedPattern_ReturnsNoRelated finds related patterns
// for a pattern that exists in PostgreSQL but has no graph node yet, which is
// a success with no related patterns.
func TestMCPFindRelated_UnenrichedPattern_ReturnsNoRelated(t *testing.T) {
	apiClient := helpers.NewTestClient(t)
	pattern := helpers.CreateTestPattern(t, apiClient)

	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": pattern.ID})

	result := helpers.AssertMCPSuccess(t, resp)
	want := "No related patterns found for '" + pattern.Name + "'"
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, want) {
		t.Errorf("expected %q in response, got: %+v", want, result.Content)
	}
}

func TestMCPFindRelated_NonExistentID_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "find_related_patterns", map[string]any{"pattern_id": uuid.New().String()})
	helpers.AssertMCPError(t, resp)
}

// Section 6: search_patterns validation

func TestMCPSearchPatterns_LimitTooLow_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "limit": 0})
	helpers.AssertMCPError(t, resp)
}

func TestMCPSearchPatterns_LimitTooHigh_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "limit": 51})
	helpers.AssertMCPError(t, resp)
}

func TestMCPSearchPatterns_ThresholdTooLow_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "threshold": -0.1})
	helpers.AssertMCPError(t, resp)
}

func TestMCPSearchPatterns_ThresholdTooHigh_ReturnsError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "test", "threshold": 1.1})
	helpers.AssertMCPError(t, resp)
}

// Section 7: search_patterns with valid query

func TestMCPSearchPatterns_ValidQuery_ReturnsResponse(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.CallTool(t, "search_patterns", map[string]any{"query": "dependency injection"})
	helpers.AssertMCPSuccess(t, resp)
}

// Section 8: Protocol correctness

func TestMCPEndpoint_InvalidMethod_ReturnsProtocolError(t *testing.T) {
	client := helpers.NewMCPClient(t)
	req := helpers.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      999,
		Method:  "nonexistent/method",
		Params:  map[string]any{},
	}
	resp := client.Post(t, req)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	var result helpers.JSONRPCResponse
	if err := json.Unmarshal(helpers.ExtractSSEData(data), &result); err != nil {
		// A non-JSON-RPC response body also indicates the method was rejected — pass.
		return
	}
	if result.Error == nil {
		t.Fatal("expected JSON-RPC error for unknown method, got none")
	}
}

func TestMCPEndpoint_MalformedJSON_DoesNotPanic(t *testing.T) {
	mcpURL := helpers.MCPBaseURL() + "/mcp"
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewBufferString(`{not valid json}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("server panicked or returned 500 on malformed JSON, status: %d", resp.StatusCode)
	}
}

// Section 9: search_patterns graph-enhanced results

// TestGraphEnhancedSearch_UnenrichedPattern_NoGraphSection creates an unenriched
// pattern via the API and searches for it via MCP. Because the pattern has no
// embeddings or graph nodes, the response must succeed vector-only, without a
// "Related Patterns (via graph)" section.
func TestGraphEnhancedSearch_UnenrichedPattern_NoGraphSection(t *testing.T) {
	apiClient := helpers.NewTestClient(t)
	pattern := helpers.CreateTestPattern(t, apiClient)

	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "search_patterns", map[string]any{
		"query": pattern.Name,
	})

	// Success path: the response must not contain a graph section for unenriched patterns.
	result := helpers.AssertMCPSuccess(t, resp)
	if len(result.Content) == 0 {
		return // no results is acceptable for an unenriched pattern
	}
	text := result.Content[0].Text
	if strings.Contains(text, "Related Patterns (via graph)") {
		t.Errorf("unenriched pattern response must not contain graph section, got:\n%s", text)
	}
}

// TestGraphEnhancedSearch_ResponseFormat_ContainsExpectedSections calls
// search_patterns with a generic query and validates the format contract of the
// response text. If a graph section is present, its required sub-elements must
// also be present.
func TestGraphEnhancedSearch_ResponseFormat_ContainsExpectedSections(t *testing.T) {
	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "search_patterns", map[string]any{
		"query": "dependency injection",
	})

	result := helpers.AssertMCPSuccess(t, resp)
	if len(result.Content) == 0 {
		t.Log("empty content returned — no format assertions to make")
		return
	}

	text := result.Content[0].Text
	if strings.HasPrefix(text, "No patterns found matching") {
		return // nothing is enriched in this suite, so an empty result is expected
	}

	// The header of a successful result must advertise the match count.
	if !strings.Contains(text, "Found") || !strings.Contains(text, "matching") {
		t.Errorf("expected response header to contain 'Found' and 'matching', got:\n%s", text)
	}

	// When a graph section is present, its required sub-fields must also appear.
	if strings.Contains(text, "Related Patterns (via graph)") {
		if !strings.Contains(text, "similarity:") {
			t.Errorf("graph section present but 'similarity:' not found in response:\n%s", text)
		}
		if !strings.Contains(text, "Found via:") {
			t.Errorf("graph section present but 'Found via:' not found in response:\n%s", text)
		}
	}
}

// TestGraphEnhancedSearch_NoDuplicatePatterns creates a pattern via the API,
// searches for it by name, and verifies that the pattern name does not appear in
// both the vector results section and the graph results section simultaneously.
// For unenriched patterns there will be no graph section, making this a structural
// guard against future deduplication regressions.
func TestGraphEnhancedSearch_NoDuplicatePatterns(t *testing.T) {
	apiClient := helpers.NewTestClient(t)
	pattern := helpers.CreateTestPattern(t, apiClient)

	mcpClient := helpers.NewMCPClient(t)
	resp := mcpClient.CallTool(t, "search_patterns", map[string]any{
		"query": pattern.Name,
	})

	result := helpers.AssertMCPSuccess(t, resp)
	if len(result.Content) == 0 {
		return // no results — nothing to check
	}

	text := result.Content[0].Text
	if !strings.Contains(text, pattern.Name) {
		return // pattern not in results — nothing to check
	}

	// Split on the graph section separator to isolate each region.
	const graphSectionHeader = "### Related Patterns (via graph)"
	parts := strings.SplitN(text, graphSectionHeader, 2)
	if len(parts) < 2 {
		// No graph section — single-region response, no duplicate possible.
		return
	}

	vectorSection := parts[0]
	graphSection := parts[1]

	if strings.Contains(vectorSection, pattern.Name) && strings.Contains(graphSection, pattern.Name) {
		t.Errorf("pattern %q appears in both vector section and graph section — deduplication failure:\n%s",
			pattern.Name, text)
	}
}

// Section 10: protocol version 2026-07-28

func TestMCPServerDiscover_AdvertisesVersionAndTools(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.Post(t, helpers.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "server/discover"})
	helpers.AssertStatusCode(t, resp, http.StatusOK)
	var out struct {
		Result struct {
			ResultType        string                     `json:"resultType"`
			SupportedVersions []string                   `json:"supportedVersions"`
			Capabilities      map[string]json.RawMessage `json:"capabilities"`
			Meta              struct {
				ServerInfo struct {
					Name string `json:"name"`
				} `json:"io.modelcontextprotocol/serverInfo"`
			} `json:"_meta"`
		} `json:"result"`
	}
	helpers.Decode(t, resp, &out)
	r := out.Result
	if r.ResultType != "complete" {
		t.Errorf("resultType = %q, want complete", r.ResultType)
	}
	if !slices.Contains(r.SupportedVersions, helpers.ProtocolVersion) {
		t.Errorf("supportedVersions %v does not include %s", r.SupportedVersions, helpers.ProtocolVersion)
	}
	if _, ok := r.Capabilities["tools"]; !ok {
		t.Errorf("capabilities %v lack tools", r.Capabilities)
	}
	if _, ok := r.Capabilities["logging"]; ok {
		t.Error("capabilities advertise the deprecated logging feature")
	}
	if r.Meta.ServerInfo.Name != "mnemonic" {
		t.Errorf("serverInfo name = %q, want mnemonic", r.Meta.ServerInfo.Name)
	}
}

func TestMCPToolsList_CarriesCacheHint(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.ListTools(t)
	if resp.Result == nil {
		t.Fatalf("ListTools returned no result: %+v", resp.Error)
	}
	r := resp.Result
	if r.ResultType != "complete" || r.TTLMs != 3600000 || r.CacheScope != "public" {
		t.Errorf("got resultType=%q ttlMs=%d cacheScope=%q, want complete/3600000/public", r.ResultType, r.TTLMs, r.CacheScope)
	}
}

func TestMCPToolsCall_HeaderMismatch_Returns400(t *testing.T) {
	client := helpers.NewMCPClient(t)
	headers := http.Header{}
	headers.Set("MCP-Protocol-Version", helpers.ProtocolVersion)
	headers.Set("Mcp-Method", "tools/call")
	headers.Set("Mcp-Name", "get_pattern")
	resp := client.PostRaw(t, helpers.JSONRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: map[string]any{
			"name":      "search_patterns",
			"arguments": map[string]any{"query": "x"},
			"_meta": map[string]any{
				"io.modelcontextprotocol/protocolVersion":    helpers.ProtocolVersion,
				"io.modelcontextprotocol/clientCapabilities": map[string]any{},
			},
		},
	}, headers)
	helpers.AssertStatusCode(t, resp, http.StatusBadRequest)
	var out helpers.JSONRPCResponse
	helpers.Decode(t, resp, &out)
	if out.Error == nil || out.Error.Code != -32020 {
		t.Errorf("error = %+v, want code -32020 (HeaderMismatch)", out.Error)
	}
}

func TestMCPEndpoint_ForeignOrigin_Returns403(t *testing.T) {
	client := helpers.NewMCPClient(t)
	headers := http.Header{}
	headers.Set("Origin", "https://evil.example")
	headers.Set("Sec-Fetch-Site", "cross-site")
	resp := client.PostRaw(t, helpers.JSONRPCRequest{JSONRPC: "2.0", ID: 1, Method: "tools/list", Params: map[string]any{}}, headers)
	defer resp.Body.Close()
	helpers.AssertStatusCode(t, resp, http.StatusForbidden)
}

func TestMCPEndpoint_GET_Returns405(t *testing.T) {
	resp, err := http.Get(helpers.MCPBaseURL() + "/mcp")
	if err != nil {
		t.Fatalf("GET /mcp: %v", err)
	}
	defer resp.Body.Close()
	helpers.AssertStatusCode(t, resp, http.StatusMethodNotAllowed)
}

// A client on an earlier protocol sends no MCP headers or _meta; it must
// still be served.
func TestMCPLegacyClient_ToolsCallStillWorks(t *testing.T) {
	client := helpers.NewMCPClient(t)
	resp := client.PostRaw(t, helpers.JSONRPCRequest{
		JSONRPC: "2.0", ID: 1, Method: "tools/call",
		Params: helpers.ToolCallParams{Name: "search_patterns", Arguments: map[string]any{"query": "error handling"}},
	}, nil)
	helpers.AssertStatusCode(t, resp, http.StatusOK)
	var out helpers.JSONRPCResponse
	helpers.Decode(t, resp, &out)
	helpers.AssertMCPSuccess(t, out)
}
