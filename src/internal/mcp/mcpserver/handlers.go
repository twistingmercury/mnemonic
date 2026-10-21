package mcpserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	patternrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/pattern"
	patternsvc "github.com/twistingmercury/mnemonic/internal/mcp/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/mcp/service/search"
)

// handleSearchPatterns returns a handler for the search_patterns tool.
// It delegates to the search service and formats results as markdown. The
// tool's input schema has already defaulted and range-checked the limit and
// threshold.
func handleSearchPatterns(search func(context.Context, searchsvc.SearchOptions) (*searchsvc.SearchResult, error), logger zerolog.Logger) mcp.ToolHandlerFor[SearchPatternsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input SearchPatternsInput) (*mcp.CallToolResult, any, error) {
		withTrace(ctx, logger).Info().
			Str("tool", "search_patterns").
			Str("query_preview", truncate(input.Query, 100)).
			Msg("mcp tool invoked")

		result, err := search(ctx, searchsvc.SearchOptions{
			Query:     input.Query,
			Limit:     input.Limit,
			Threshold: input.Threshold,
			Tags:      input.Tags,
			Language:  input.Language,
			Domain:    input.Domain,
		})
		if err != nil {
			return nil, nil, mapServiceError(err)
		}

		markdown := formatSearchResults(result)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: markdown}},
		}, nil, nil
	}
}

// handleFindRelatedPatterns returns a handler for the find_related_patterns tool.
// It validates the pattern UUID, delegates to the pattern service, and formats
// results as markdown. The tool's input schema has already defaulted and
// range-checked the limit.
func handleFindRelatedPatterns(findRelated func(context.Context, uuid.UUID, int) (string, []patternsvc.RelatedPatternResult, error), logger zerolog.Logger) mcp.ToolHandlerFor[FindRelatedPatternsInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input FindRelatedPatternsInput) (*mcp.CallToolResult, any, error) {
		patternID, err := uuid.Parse(input.PatternID)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: invalid UUID %q", ErrInvalidInput, input.PatternID)
		}

		withTrace(ctx, logger).Info().
			Str("tool", "find_related_patterns").
			Str("pattern_id", patternID.String()).
			Msg("mcp tool invoked")

		sourceName, results, err := findRelated(ctx, patternID, input.Limit)
		if err != nil {
			return nil, nil, mapServiceError(err)
		}

		markdown := formatRelatedPatterns(sourceName, results)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: markdown}},
		}, nil, nil
	}
}

// handleGetPattern returns a handler for the get_pattern tool.
// It validates the pattern UUID, retrieves the pattern with graph context,
// and formats the result as markdown.
func handleGetPattern(getPattern func(context.Context, uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error), logger zerolog.Logger) mcp.ToolHandlerFor[GetPatternInput, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input GetPatternInput) (*mcp.CallToolResult, any, error) {
		id, err := uuid.Parse(input.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: invalid UUID %q", ErrInvalidInput, input.ID)
		}

		withTrace(ctx, logger).Info().
			Str("tool", "get_pattern").
			Str("pattern_id", id.String()).
			Msg("mcp tool invoked")

		pattern, graphCtx, err := getPattern(ctx, id)
		if err != nil {
			return nil, nil, mapServiceError(err)
		}

		markdown := formatPattern(pattern, graphCtx)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: markdown}},
		}, nil, nil
	}
}

// mapServiceError translates service-layer errors to MCP-layer sentinel
// errors: unknown patterns are not found, everything else is unavailable.
func mapServiceError(err error) error {
	if errors.Is(err, patternrepo.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrPatternNotFound, err)
	}
	return fmt.Errorf("%w: %v", ErrServiceUnavailable, err)
}

// truncate shortens a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
