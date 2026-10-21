package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// apiBaseURL returns the base URL for the API from environment or default.
func apiBaseURL() string {
	if url := os.Getenv("API_URL"); url != "" {
		return url
	}
	return "http://localhost:8080"
}

// TestClient wraps an HTTP client with authentication headers and helper methods.
type TestClient struct {
	*http.Client
	BaseURL   string
	UserID    string
	TeamID    string
	UserRoles string
	RequestID string
}

// NewTestClient creates a new test client with default authentication headers.
// By default, creates a client with admin role for full access.
func NewTestClient(t *testing.T) *TestClient {
	t.Helper()
	return &TestClient{
		Client:    &http.Client{Timeout: 10 * time.Second},
		BaseURL:   apiBaseURL(),
		UserID:    uuid.New().String(),
		TeamID:    uuid.New().String(),
		UserRoles: "admin,developer",
		RequestID: uuid.New().String(),
	}
}

// Do executes an HTTP request with authentication headers.
func (c *TestClient) Do(req *http.Request) (*http.Response, error) {
	if c.UserID != "" {
		req.Header.Set("X-User-ID", c.UserID)
	}
	if c.TeamID != "" {
		req.Header.Set("X-Team-ID", c.TeamID)
	}
	if c.UserRoles != "" {
		req.Header.Set("X-User-Roles", c.UserRoles)
	}
	if c.RequestID != "" {
		req.Header.Set("X-Request-ID", c.RequestID)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return c.Client.Do(req)
}

// Post performs a POST request with JSON body.
func (c *TestClient) Post(path string, body any) (*http.Response, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.BaseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	return c.Do(req)
}

// ReadBody reads and closes the response body.
func ReadBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}
	return body
}

// ParseJSON unmarshals JSON response body into the provided struct.
func ParseJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var result T
	body := ReadBody(t, resp)
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nBody: %s", err, string(body))
	}
	return result
}

// AssertStatusCode verifies the response status code matches expected.
func AssertStatusCode(t *testing.T, resp *http.Response, expected int) {
	t.Helper()
	if resp.StatusCode != expected {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected status %d, got %d\nBody: %s", expected, resp.StatusCode, string(body))
	}
}

// GenerateUniqueName creates a unique name for test resources.
func GenerateUniqueName(prefix string) string {
	return fmt.Sprintf("%s-%s", prefix, uuid.New().String()[:8])
}

// ExtractSSEData extracts the JSON payload from an SSE-framed response body.
// It scans lines for the first "data: " prefix and returns the remainder.
// If no data line is found, the original bytes are returned unchanged.
func ExtractSSEData(body []byte) []byte {
	for line := range bytes.SplitSeq(body, []byte("\n")) {
		if data, ok := bytes.CutPrefix(line, []byte("data: ")); ok {
			return data
		}
	}
	return body
}

// Decode reads an SSE- or JSON-framed JSON-RPC response into out and closes
// the body.
func Decode(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	data := ReadBody(t, resp)
	if err := json.Unmarshal(ExtractSSEData(data), out); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, data)
	}
}

// MCPBaseURL returns the base URL for the MCP server from environment or default.
func MCPBaseURL() string {
	if url := os.Getenv("MCP_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// MCPClient wraps an HTTP client for issuing JSON-RPC requests to the MCP endpoint.
type MCPClient struct {
	client  *http.Client
	baseURL string
	nextID  int
}

// NewMCPClient creates a new MCP client pointed at the configured MCP base URL.
func NewMCPClient(t *testing.T) *MCPClient {
	t.Helper()
	return &MCPClient{
		client:  &http.Client{Timeout: 10 * time.Second},
		baseURL: MCPBaseURL(),
		nextID:  1,
	}
}

// ProtocolVersion is the MCP protocol version the client speaks.
const ProtocolVersion = "2026-07-28"

// Post sends req to /mcp as a 2026-07-28 request: the protocol version, client
// info and capabilities go in params._meta, and the method (plus the tool name
// for tools/call) is mirrored into the Mcp-Method and Mcp-Name headers.
func (c *MCPClient) Post(t *testing.T, req JSONRPCRequest) *http.Response {
	t.Helper()
	params := map[string]any{}
	if req.Params != nil {
		raw, err := json.Marshal(req.Params)
		if err != nil {
			t.Fatalf("MCPClient: marshal params: %v", err)
		}
		if err := json.Unmarshal(raw, &params); err != nil {
			t.Fatalf("MCPClient: params must be an object: %v", err)
		}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "mnemonic-e2e", "version": "0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	req.Params = params
	headers := http.Header{}
	headers.Set("MCP-Protocol-Version", ProtocolVersion)
	headers.Set("Mcp-Method", req.Method)
	if name, ok := params["name"].(string); ok {
		headers.Set("Mcp-Name", name)
	}
	return c.PostRaw(t, req, headers)
}

// PostRaw marshals body as JSON and POSTs it to /mcp with only the given
// headers added, returning the raw response. Use it for legacy or malformed
// requests.
func (c *MCPClient) PostRaw(t *testing.T, body any, headers http.Header) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("MCPClient: marshal request: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/mcp", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("MCPClient: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header[k] = v
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatalf("MCPClient: POST /mcp: %v", err)
	}
	return resp
}

// CallTool sends a tools/call JSON-RPC request and returns the decoded response.
func (c *MCPClient) CallTool(t *testing.T, toolName string, arguments any) JSONRPCResponse {
	t.Helper()
	id := c.nextID
	c.nextID++
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/call",
		Params:  ToolCallParams{Name: toolName, Arguments: arguments},
	}
	var result JSONRPCResponse
	Decode(t, c.Post(t, req), &result)
	return result
}

// ListTools sends a tools/list JSON-RPC request and returns the decoded response.
func (c *MCPClient) ListTools(t *testing.T) JSONRPCToolsListResp {
	t.Helper()
	id := c.nextID
	c.nextID++
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  "tools/list",
		Params:  map[string]any{},
	}
	var result JSONRPCToolsListResp
	Decode(t, c.Post(t, req), &result)
	return result
}

// AssertMCPSuccess asserts the response is a non-error MCP tool result and returns it.
func AssertMCPSuccess(t *testing.T, resp JSONRPCResponse) MCPToolResult {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected MCP success, got error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
	if resp.Result == nil {
		t.Fatal("expected MCP result, got nil")
	}
	if resp.Result.IsError {
		text := ""
		if len(resp.Result.Content) > 0 {
			text = resp.Result.Content[0].Text
		}
		t.Fatalf("expected MCP success result, got isError=true: %s", text)
	}
	return *resp.Result
}

// AssertMCPError asserts the response is either a JSON-RPC error or a tool-level error result.
func AssertMCPError(t *testing.T, resp JSONRPCResponse) {
	t.Helper()
	if resp.Error != nil {
		return // JSON-RPC level error — ok
	}
	if resp.Result != nil && resp.Result.IsError {
		return // tool-level error — also ok
	}
	t.Fatal("expected MCP error response, but got success")
}

// CreateTestPattern creates a pattern via the REST API and returns it.
// It registers no cleanup — callers own lifetime management if needed.
func CreateTestPattern(t *testing.T, client *TestClient) Pattern {
	t.Helper()
	body := PatternCreate{
		Name:       GenerateUniqueName("mcp-test"),
		Content:    "# Test\n\nTest pattern content.",
		EntityType: "go-pattern",
		Language:   "go",
		Domain:     "backend",
	}
	resp, err := client.Post("/v1/api/patterns", body)
	if err != nil {
		t.Fatalf("createTestPattern: POST /v1/api/patterns: %v", err)
	}
	AssertStatusCode(t, resp, http.StatusAccepted)
	return ParseJSON[Pattern](t, resp)
}
