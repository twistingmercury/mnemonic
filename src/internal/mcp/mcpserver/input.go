package mcpserver

// SearchPatternsInput defines the parameters for the search_patterns tool.
// Its input schema is inferred from the jsonschema struct tags; fields without
// omitempty are required. searchPatternsTool adds the defaults and bounds of
// Limit and Threshold, which the SDK fills in and checks before the handler
// runs, so an omitted value arrives as its default and an explicit 0 as 0.
type SearchPatternsInput struct {
	Query     string   `json:"query"              jsonschema:"Natural language search query"`
	Limit     int      `json:"limit,omitempty"    jsonschema:"Maximum number of results to return (default 10, max 50)"`
	Threshold float64  `json:"threshold,omitempty" jsonschema:"Minimum cosine similarity score 0.0-1.0 (default 0.5)"`
	Tags      []string `json:"tags,omitempty"      jsonschema:"Conjunctive (AND) filter by tag. Pattern must contain ALL specified tags."`
	Language  string   `json:"language,omitempty"  jsonschema:"Optional: filter results by pattern language (e.g. 'go', 'python')"`
	Domain    string   `json:"domain,omitempty"    jsonschema:"Optional: filter results by pattern domain (e.g. 'backend', 'testing')"`
}

// FindRelatedPatternsInput defines the parameters for the find_related_patterns
// tool. findRelatedPatternsTool adds the default and bounds of Limit.
type FindRelatedPatternsInput struct {
	PatternID string `json:"pattern_id"      jsonschema:"UUID of the pattern to find relations for"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Maximum number of related patterns to return (default 5, max 20)"`
}

// GetPatternInput defines the parameters for the get_pattern tool.
type GetPatternInput struct {
	ID string `json:"id" jsonschema:"Pattern UUID to fetch"`
}
