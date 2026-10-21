package helpers

// ErrorResponse represents RFC 7807 Problem Details error response.
// OpenAPI: api/openapi/mnemonic-v1.yaml:152 (ErrorResponse)
type ErrorResponse struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Status   int          `json:"status"`
	Detail   string       `json:"detail,omitempty"`
	Instance string       `json:"instance,omitempty"`
	TraceID  string       `json:"traceId"`
	Errors   []FieldError `json:"errors,omitempty"`
}

// FieldError represents individual field validation error.
// OpenAPI: api/openapi/mnemonic-v1.yaml:229 (FieldError)
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Pagination represents cursor-based pagination metadata.
// OpenAPI: api/openapi/mnemonic-v1.yaml:265 (Pagination)
type Pagination struct {
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// Pattern represents the full pattern definition.
// OpenAPI: api/openapi/mnemonic-v1.yaml:550 (Pattern)
type Pattern struct {
	ID               string   `json:"id,omitempty"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	Content          string   `json:"content"`
	Tags             []string `json:"tags,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	UpdatedAt        string   `json:"updated_at,omitempty"`
	EnrichmentStatus string   `json:"enrichment_status,omitempty"`
	EnrichmentError  string   `json:"enrichment_error,omitempty"`
	EnrichedAt       string   `json:"enriched_at,omitempty"`
	EntityType       string   `json:"entity_type,omitempty"`
	Language         string   `json:"language,omitempty"`
	Domain           string   `json:"domain,omitempty"`
	Version          string   `json:"version,omitempty"`
	RelatedPatterns  []string `json:"related_patterns,omitempty"`
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

// PatternUpdate represents request body for updating a pattern.
// OpenAPI: api/openapi/mnemonic-v1.yaml:683 (PatternUpdate)
type PatternUpdate struct {
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

// PatternSummary represents pattern summary without content.
// OpenAPI: api/openapi/mnemonic-v1.yaml:711 (PatternSummary)
type PatternSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// PatternList represents paginated list of patterns.
// OpenAPI: api/openapi/mnemonic-v1.yaml:752 (PatternList)
type PatternList struct {
	Data       []PatternSummary `json:"data"`
	Pagination Pagination       `json:"pagination"`
}

// PatternSearchResult represents a single chunk-based semantic search result.
type PatternSearchResult struct {
	PatternID    string   `json:"pattern_id"`
	PatternName  string   `json:"pattern_name"`
	EntityType   string   `json:"entity_type"`
	Language     string   `json:"language"`
	Domain       string   `json:"domain"`
	Tags         []string `json:"tags"`
	SectionTitle string   `json:"section_title"`
	ChunkIndex   int      `json:"chunk_index"`
	Content      string   `json:"content"`
	Similarity   float64  `json:"similarity"`
}

// PatternSearchMetadata contains metadata about a semantic search operation.
// OpenAPI: api/openapi/mnemonic-v1.yaml:752 (PatternSearchResponse.metadata)
type PatternSearchMetadata struct {
	Query            string `json:"query"`
	TotalCandidates  int    `json:"total_candidates"`
	SearchDurationMs int    `json:"search_duration_ms"`
}

// PatternSearchResponse represents the full semantic search response.
// OpenAPI: api/openapi/mnemonic-v1.yaml:740 (PatternSearchResponse)
type PatternSearchResponse struct {
	Results  []PatternSearchResult `json:"results"`
	Metadata PatternSearchMetadata `json:"metadata"`
}

// ChunkSummary represents a pattern chunk summary (chunk_index, section_title, enrichment_status).
type ChunkSummary struct {
	ChunkIndex       int    `json:"chunk_index"`
	SectionTitle     string `json:"section_title"`
	EnrichmentStatus string `json:"enrichment_status"`
}

// ChunkListResponse is the response body for GET /v1/api/patterns/:id/chunks.
type ChunkListResponse struct {
	Chunks []ChunkSummary `json:"chunks"`
	Count  int            `json:"count"`
}

// VersionResponse represents version information response from GET /version.
// Fields match the handler in internal/handlers/operations/operations.go.
type VersionResponse struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	BuildDate string `json:"build_date"`
	Commit    string `json:"commit"`
}
