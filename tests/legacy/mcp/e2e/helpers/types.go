package helpers

// Pattern holds the fields the tests read from a created pattern.
// OpenAPI: api/openapi/mnemonic-v1.yaml:550 (Pattern)
type Pattern struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PatternCreate represents request body for creating a new pattern.
// OpenAPI: api/openapi/mnemonic-v1.yaml:644 (PatternCreate)
type PatternCreate struct {
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Content         string   `json:"content"`
	Tags            []string `json:"tags,omitempty"`
	EntityType      string   `json:"entity_type,omitempty"`
	Language        string   `json:"language,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	Version         string   `json:"version,omitempty"`
	RelatedPatterns []string `json:"related_patterns,omitempty"`
}

// JSON-RPC / MCP types

// JSONRPCRequest is the envelope for a JSON-RPC 2.0 request sent to the MCP endpoint.
type JSONRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// ToolCallParams carries the tool name and its arguments for a tools/call request.
type ToolCallParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

// JSONRPCError represents the error object inside a JSON-RPC 2.0 error response.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPContent is a single content item returned inside an MCP tool result.
type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// MCPToolResult is the result payload of a successful tools/call response.
type MCPToolResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError"`
}

// JSONRPCResponse is the envelope for a JSON-RPC 2.0 response from the MCP endpoint.
type JSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int            `json:"id"`
	Result  *MCPToolResult `json:"result,omitempty"`
	Error   *JSONRPCError  `json:"error,omitempty"`
}

// ToolDefinition describes a single tool exposed by the MCP server.
type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToolsListResult is the result payload of a tools/list response.
type ToolsListResult struct {
	ResultType string           `json:"resultType"`
	TTLMs      int              `json:"ttlMs"`
	CacheScope string           `json:"cacheScope"`
	Tools      []ToolDefinition `json:"tools"`
}

// JSONRPCToolsListResp is the envelope for a JSON-RPC 2.0 tools/list response.
type JSONRPCToolsListResp struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      int              `json:"id"`
	Result  *ToolsListResult `json:"result,omitempty"`
	Error   *JSONRPCError    `json:"error,omitempty"`
}
