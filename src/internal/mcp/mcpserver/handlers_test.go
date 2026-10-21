package mcpserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	patternrepo "github.com/twistingmercury/librarian-mcp/internal/repository/pattern"
	patternsvc "github.com/twistingmercury/librarian-mcp/internal/service/pattern"
	searchsvc "github.com/twistingmercury/librarian-mcp/internal/service/search"
)

// searchStub returns a SearchPatterns func that checks the options it gets
// against want, when want is non-nil, and returns result and err.
func searchStub(t *testing.T, want *searchsvc.SearchOptions, result *searchsvc.SearchResult, err error) func(context.Context, searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
	t.Helper()
	return func(_ context.Context, opts searchsvc.SearchOptions) (*searchsvc.SearchResult, error) {
		if want != nil {
			assert.Equal(t, *want, opts)
		}
		return result, err
	}
}

// findRelatedStub returns a FindRelatedPatterns func that checks the pattern
// ID and limit it gets and returns name, results and err.
func findRelatedStub(t *testing.T, wantID uuid.UUID, wantLimit int, name string, results []patternsvc.RelatedPatternResult, err error) func(context.Context, uuid.UUID, int) (string, []patternsvc.RelatedPatternResult, error) {
	t.Helper()
	return func(_ context.Context, id uuid.UUID, limit int) (string, []patternsvc.RelatedPatternResult, error) {
		assert.Equal(t, wantID, id)
		assert.Equal(t, wantLimit, limit)
		return name, results, err
	}
}

// getPatternStub returns a GetPatternWithGraph func that checks the ID it gets
// and returns pattern, graphCtx and err.
func getPatternStub(t *testing.T, wantID uuid.UUID, pattern *patternrepo.Pattern, graphCtx *patternsvc.GraphContext, err error) func(context.Context, uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error) {
	t.Helper()
	return func(_ context.Context, id uuid.UUID) (*patternrepo.Pattern, *patternsvc.GraphContext, error) {
		assert.Equal(t, wantID, id)
		return pattern, graphCtx, err
	}
}

// extractTextContent extracts the text string from a CallToolResult's Content.
func extractTextContent(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	require.NotNil(t, result)
	require.NotEmpty(t, result.Content)
	data, err := result.Content[0].MarshalJSON()
	require.NoError(t, err)
	return string(data)
}

// --- search_patterns handler tests ---

func TestHandleSearchPatterns_HappyPath(t *testing.T) {
	t.Parallel()

	searchResult := &searchsvc.SearchResult{
		Query: "error handling",
		Matches: []*searchsvc.ChunkMatch{
			{PatternID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), PatternName: "go-error-handling", Tags: []string{"go", "errors"}, Content: "Error handling content", Similarity: 0.92},
			{PatternID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), PatternName: "retry-logic", Tags: []string{"go", "resilience"}, Content: "Retry logic content", Similarity: 0.85},
		},
	}

	handler := handleSearchPatterns(searchStub(t, &searchsvc.SearchOptions{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.5,
	}, searchResult, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "error handling",
		Limit:     10,
		Threshold: 0.5,
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	// 2 sections from 2 distinct patterns.
	assert.Contains(t, text, "Found 2 sections across 2 patterns matching 'error handling'")
	assert.Contains(t, text, "go-error-handling (92% match)")
	assert.Contains(t, text, "retry-logic (85% match)")
}

func TestHandleSearchPatterns_NoResults(t *testing.T) {
	t.Parallel()

	searchResult := &searchsvc.SearchResult{
		Query:   "nonexistent topic",
		Matches: []*searchsvc.ChunkMatch{},
	}

	handler := handleSearchPatterns(searchStub(t, nil, searchResult, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "nonexistent topic",
		Limit:     10,
		Threshold: 0.5,
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "No patterns found matching 'nonexistent topic'")
}

func TestHandleSearchPatterns_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	handler := handleSearchPatterns(searchStub(t, nil, nil, errors.New("embedding failed")), zerolog.Nop())
	_, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "anything",
		Limit:     10,
		Threshold: 0.5,
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServiceUnavailable)
}

// TestHandleSearchPatterns_PassesOptions verifies that every input field
// reaches the search service unchanged.
func TestHandleSearchPatterns_PassesOptions(t *testing.T) {
	t.Parallel()

	handler := handleSearchPatterns(searchStub(t, &searchsvc.SearchOptions{
		Query:     "go patterns",
		Limit:     5,
		Threshold: 0.9,
		Tags:      []string{"go", "errors"},
		Language:  "python",
		Domain:    "backend",
	}, &searchsvc.SearchResult{Query: "go patterns"}, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, SearchPatternsInput{
		Query:     "go patterns",
		Limit:     5,
		Threshold: 0.9,
		Tags:      []string{"go", "errors"},
		Language:  "python",
		Domain:    "backend",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

// --- find_related_patterns handler tests ---

func TestHandleFindRelatedPatterns_HappyPath(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	relatedID := uuid.New()

	relatedResults := []patternsvc.RelatedPatternResult{
		{
			ID:             relatedID,
			Name:           "related-pattern",
			Relationship:   "RELATED_TO",
			Similarity:     0.85,
			SharedConcepts: []string{"error handling", "retry logic"},
		},
	}

	handler := handleFindRelatedPatterns(findRelatedStub(t, patternID, 5, "source-pattern", relatedResults, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		Limit:     5,
		PatternID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "Found 1 patterns related to 'source-pattern'")
	assert.Contains(t, text, "related-pattern (similarity: 0.85)")
	assert.Contains(t, text, "error handling, retry logic")
}

func TestHandleFindRelatedPatterns_NotFound(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	handler := handleFindRelatedPatterns(findRelatedStub(t, patternID, 5, "", nil, errors.Join(patternrepo.ErrNotFound, errors.New("pattern not found"))), zerolog.Nop())
	_, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		Limit:     5,
		PatternID: patternID.String(),
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatternNotFound)
}

func TestHandleFindRelatedPatterns_EmptyResults(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	handler := handleFindRelatedPatterns(findRelatedStub(t, patternID, 5, "lonely-pattern", []patternsvc.RelatedPatternResult{}, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		Limit:     5,
		PatternID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "No related patterns found for 'lonely-pattern'")
}

func TestHandleFindRelatedPatterns_InvalidUUID(t *testing.T) {
	t.Parallel()

	handler := handleFindRelatedPatterns(nil, zerolog.Nop())
	_, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: "not-a-uuid",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "invalid UUID")
}

func TestHandleFindRelatedPatterns_CustomLimit(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	limit := 10
	handler := handleFindRelatedPatterns(findRelatedStub(t, patternID, 10, "test-pattern", []patternsvc.RelatedPatternResult{}, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		PatternID: patternID.String(),
		Limit:     limit,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
}

func TestHandleFindRelatedPatterns_ServiceUnavailable(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	handler := handleFindRelatedPatterns(findRelatedStub(t, patternID, 5, "", nil, errors.New("neo4j down")), zerolog.Nop())
	_, _, err := handler(context.Background(), nil, FindRelatedPatternsInput{
		Limit:     5,
		PatternID: patternID.String(),
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrServiceUnavailable)
}

// --- get_pattern handler tests ---

func TestHandleGetPattern_HappyPathWithGraph(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	enrichedAt := time.Date(2025, 1, 10, 8, 5, 0, 0, time.UTC)
	pattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "go-error-handling",
		Tags:             []string{"go", "errors", "best-practices"},
		Content:          "Full pattern content here",
		EnrichmentStatus: "enriched",
		EnrichedAt:       &enrichedAt,
	}
	graphCtx := &patternsvc.GraphContext{
		RelatedPatterns: []patternsvc.RelatedPatternResult{
			{Name: "related-pattern", Relationship: "RELATED_TO", Similarity: 0.85},
		},
		Concepts: []patternsvc.ConceptResult{
			{Name: "error-wrapping", Type: "practice"},
		},
	}

	handler := handleGetPattern(getPatternStub(t, patternID, pattern, graphCtx, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "go-error-handling")
	assert.Contains(t, text, patternID.String())
	assert.Contains(t, text, "go, errors, best-practices")
	assert.Contains(t, text, "enriched (2025-01-10T08:05:00Z)")
	assert.Contains(t, text, "Full pattern content here")
	assert.Contains(t, text, "Related Patterns")
	assert.Contains(t, text, "related-pattern")
	assert.Contains(t, text, "Extracted Concepts")
	assert.Contains(t, text, "error-wrapping")
}

func TestHandleGetPattern_PendingEnrichment(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	pattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "pending-pattern",
		Content:          "Some content",
		EnrichmentStatus: "pending",
	}

	handler := handleGetPattern(getPatternStub(t, patternID, pattern, nil, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "pending-pattern")
	assert.Contains(t, text, "pending")
	assert.NotContains(t, text, "Related Patterns")
	assert.NotContains(t, text, "Extracted Concepts")
}

func TestHandleGetPattern_FailedEnrichment(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	errMsg := "embedding generation timed out after 30s"
	pattern := &patternrepo.Pattern{
		ID:               patternID,
		Name:             "failed-pattern",
		Content:          "Some content",
		EnrichmentStatus: "failed",
		EnrichmentError:  &errMsg,
	}

	handler := handleGetPattern(getPatternStub(t, patternID, pattern, nil, nil), zerolog.Nop())
	result, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.NoError(t, err)
	text := extractTextContent(t, result)
	assert.Contains(t, text, "failed -- embedding generation timed out after 30s")
	assert.NotContains(t, text, "Related Patterns")
	assert.NotContains(t, text, "Extracted Concepts")
}

func TestHandleGetPattern_NotFound(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	handler := handleGetPattern(getPatternStub(t, patternID, nil, nil, errors.Join(patternrepo.ErrNotFound, errors.New("pattern not found"))), zerolog.Nop())
	_, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: patternID.String(),
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrPatternNotFound)
}

func TestHandleGetPattern_InvalidUUID(t *testing.T) {
	t.Parallel()

	handler := handleGetPattern(nil, zerolog.Nop())
	_, _, err := handler(context.Background(), nil, GetPatternInput{
		ID: "bad-uuid",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "invalid UUID")
}

// --- mapServiceError tests ---

// --- truncate tests ---

func TestTruncate_ShortString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", truncate("hello", 10))
}

func TestTruncate_LongString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello...", truncate("hello world", 5))
}

func TestTruncate_ExactLength(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "hello", truncate("hello", 5))
}
