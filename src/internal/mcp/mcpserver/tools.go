package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultSearchLimit = 10
	maxSearchLimit     = 50

	defaultRelatedLimit = 5
	maxRelatedLimit     = 20
)

// searchPatternsTool returns the search_patterns tool. Its input schema
// carries the limit and threshold defaults and bounds, which the SDK applies
// to the arguments and enforces before the handler runs; defaultThreshold is
// the configured mcp.default_search_threshold.
func searchPatternsTool(defaultThreshold float64) *mcp.Tool {
	schema := inputSchema[SearchPatternsInput]()
	setRange(schema, "limit", 1, maxSearchLimit, defaultSearchLimit)
	setRange(schema, "threshold", 0, 1, defaultThreshold)
	return &mcp.Tool{
		Name:        "search_patterns",
		Description: "Semantic search over the team knowledge graph. Returns patterns ranked by vector similarity, with optional tag/language/domain filters.",
		InputSchema: schema,
	}
}

// findRelatedPatternsTool returns the find_related_patterns tool, with the
// limit default and bounds in its input schema.
func findRelatedPatternsTool() *mcp.Tool {
	schema := inputSchema[FindRelatedPatternsInput]()
	setRange(schema, "limit", 1, maxRelatedLimit, defaultRelatedLimit)
	return &mcp.Tool{
		Name:        "find_related_patterns",
		Description: "Find patterns related to a given pattern via the knowledge graph. Traverses RELATED_TO edges in Neo4j and returns patterns ranked by concept overlap strength.",
		InputSchema: schema,
	}
}

var getPatternTool = &mcp.Tool{
	Name:        "get_pattern",
	Description: "Retrieve a specific pattern by ID. Returns full pattern content, metadata, related patterns, and extracted concepts. Graph relationships are omitted when enrichment is still pending.",
}

// inputSchema infers the JSON schema for a tool's input struct.
func inputSchema[T any]() *jsonschema.Schema {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		// The input types are fixed, so this fails only on a programming error.
		panic(fmt.Sprintf("inferring input schema: %v", err))
	}
	return schema
}

// setRange sets the inclusive bounds and the default of a numeric property.
func setRange(schema *jsonschema.Schema, property string, minimum, maximum, def float64) {
	p := schema.Properties[property]
	p.Minimum = &minimum
	p.Maximum = &maximum
	p.Default = json.RawMessage(fmt.Sprint(def))
}
