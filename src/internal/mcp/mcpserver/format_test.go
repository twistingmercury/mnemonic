package mcpserver

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	patternrepo "github.com/twistingmercury/mnemonic/internal/mcp/repository/pattern"
	patternsvc "github.com/twistingmercury/mnemonic/internal/mcp/service/pattern"
	searchsvc "github.com/twistingmercury/mnemonic/internal/mcp/service/search"
)

// --- formatSearchResults tests ---

func TestFormatSearchResults_WithResults(t *testing.T) {
	t.Parallel()

	result := &searchsvc.SearchResult{
		Query: "error handling",
		Matches: []*searchsvc.ChunkMatch{
			{
				PatternID:    uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
				PatternName:  "go-error-handling",
				SectionTitle: "Overview",
				Tags:         []string{"go", "errors", "best-practices"},
				Content:      "Always wrap errors with context.",
				Similarity:   0.92,
			},
			{
				PatternID:    uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
				PatternName:  "retry-logic",
				SectionTitle: "Overview",
				Tags:         []string{"go", "resilience"},
				Content:      "Use exponential backoff.",
				Similarity:   0.847,
			},
		},
	}

	md := formatSearchResults(result)

	// 2 sections from 2 distinct patterns.
	assert.Contains(t, md, "Found 2 sections across 2 patterns matching 'error handling':")
	assert.Contains(t, md, "## go-error-handling (92% match)")
	assert.Contains(t, md, "**Tags:** go, errors, best-practices")
	assert.Contains(t, md, "Always wrap errors with context.")
	assert.Contains(t, md, "## retry-logic (85% match)")
	assert.Contains(t, md, "Use exponential backoff.")
}

func TestFormatSearchResults_Empty(t *testing.T) {
	t.Parallel()

	result := &searchsvc.SearchResult{
		Query:   "nonexistent",
		Matches: []*searchsvc.ChunkMatch{},
	}

	md := formatSearchResults(result)

	assert.Equal(t, "No patterns found matching 'nonexistent'.", md)
}

func TestFormatSearchResults_NoTags(t *testing.T) {
	t.Parallel()

	result := &searchsvc.SearchResult{
		Query: "query",
		Matches: []*searchsvc.ChunkMatch{
			{
				PatternID:   uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"),
				PatternName: "no-tags",
				Content:     "Content without tags",
				Similarity:  0.75,
			},
		},
	}

	md := formatSearchResults(result)

	assert.Contains(t, md, "## no-tags (75% match)")
	assert.NotContains(t, md, "**Tags:**")
}

func TestFormatSearchResults_MultipleChunksSamePattern(t *testing.T) {
	t.Parallel()

	// Two chunks from the same pattern and one from a different pattern: the
	// header should report 3 sections across 2 patterns.
	sharedID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	otherID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")

	result := &searchsvc.SearchResult{
		Query: "error handling",
		Matches: []*searchsvc.ChunkMatch{
			{PatternID: sharedID, PatternName: "go-errors", SectionTitle: "Overview", Content: "Part one.", Similarity: 0.9},
			{PatternID: sharedID, PatternName: "go-errors", SectionTitle: "Details", Content: "Part two.", Similarity: 0.85},
			{PatternID: otherID, PatternName: "retry-logic", SectionTitle: "Overview", Content: "Retry.", Similarity: 0.8},
		},
	}

	md := formatSearchResults(result)

	assert.Contains(t, md, "Found 3 sections across 2 patterns matching 'error handling':")
}

func TestFormatSearchResults_SimilarityRounding(t *testing.T) {
	t.Parallel()

	result := &searchsvc.SearchResult{
		Query: "query",
		Matches: []*searchsvc.ChunkMatch{
			{
				PatternID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				PatternName: "pattern-a",
				Content:     "Content",
				Similarity:  0.925,
			},
			{
				PatternID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
				PatternName: "pattern-b",
				Content:     "Content",
				Similarity:  0.994,
			},
		},
	}

	md := formatSearchResults(result)

	// 0.925 rounds to 93, 0.994 rounds to 99.
	assert.Contains(t, md, "93% match")
	assert.Contains(t, md, "99% match")
}

func TestFormatSearchResults_WithGraphMatches(t *testing.T) {
	t.Parallel()

	seedID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	graphID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	result := &searchsvc.SearchResult{
		Query: "error handling",
		Matches: []*searchsvc.ChunkMatch{
			{
				PatternID:   seedID,
				PatternName: "go-error-handling",
				Content:     "Always wrap errors with context.",
				Similarity:  0.92,
			},
		},
		GraphMatches: []*searchsvc.GraphMatch{
			{
				PatternID:       graphID,
				PatternName:     "circuit-breaker",
				Similarity:      0.78,
				ConceptNames:    []string{"resilience", "fault-tolerance"},
				SeedPatternName: "go-error-handling",
			},
		},
	}

	md := formatSearchResults(result)

	// Vector section present.
	assert.Contains(t, md, "## go-error-handling (92% match)")
	assert.Contains(t, md, "Always wrap errors with context.")

	// Graph section present.
	assert.Contains(t, md, "### Related Patterns (via graph)")
	assert.Contains(t, md, "## circuit-breaker (similarity: 0.78)")
	assert.Contains(t, md, "**Found via:** go-error-handling")
	assert.Contains(t, md, "**Shared concepts:** resilience, fault-tolerance")
}

// TestFormatSearchResults_NoGraphMatches verifies that nil and empty graph
// matches both leave out the graph section.
func TestFormatSearchResults_NoGraphMatches(t *testing.T) {
	t.Parallel()

	for name, graphMatches := range map[string][]*searchsvc.GraphMatch{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result := &searchsvc.SearchResult{
				Query: "error handling",
				Matches: []*searchsvc.ChunkMatch{
					{
						PatternID:   uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
						PatternName: "go-error-handling",
						Content:     "Always wrap errors with context.",
						Similarity:  0.92,
					},
				},
				GraphMatches: graphMatches,
			}

			md := formatSearchResults(result)

			assert.Contains(t, md, "## go-error-handling (92% match)")
			assert.NotContains(t, md, "Related Patterns (via graph)")
		})
	}
}

// --- formatRelatedPatterns tests ---

func TestFormatRelatedPatterns_WithResults(t *testing.T) {
	t.Parallel()

	results := []patternsvc.RelatedPatternResult{
		{
			ID:             uuid.New(),
			Name:           "related-pattern",
			Relationship:   "RELATED_TO",
			Similarity:     0.85,
			SharedConcepts: []string{"error handling", "retry logic"},
		},
		{
			ID:             uuid.New(),
			Name:           "another-related",
			Relationship:   "RELATED_TO",
			Similarity:     0.72,
			SharedConcepts: []string{"observability"},
		},
	}

	md := formatRelatedPatterns("source-pattern", results)

	assert.Contains(t, md, "Found 2 patterns related to 'source-pattern':")
	assert.Contains(t, md, "## related-pattern (similarity: 0.85)")
	assert.Contains(t, md, "**Relationship:** RELATED_TO")
	assert.Contains(t, md, "**Shared concepts:** error handling, retry logic")
	assert.Contains(t, md, "## another-related (similarity: 0.72)")
	assert.Contains(t, md, "**Shared concepts:** observability")
}

func TestFormatRelatedPatterns_Empty(t *testing.T) {
	t.Parallel()

	md := formatRelatedPatterns("lonely-pattern", []patternsvc.RelatedPatternResult{})

	assert.Equal(t, "No related patterns found for 'lonely-pattern'.", md)
}

func TestFormatRelatedPatterns_NoSharedConcepts(t *testing.T) {
	t.Parallel()

	results := []patternsvc.RelatedPatternResult{
		{
			Name:         "some-pattern",
			Relationship: "RELATED_TO",
			Similarity:   0.6,
		},
	}

	md := formatRelatedPatterns("source", results)

	assert.Contains(t, md, "## some-pattern (similarity: 0.60)")
	assert.Contains(t, md, "**Relationship:** RELATED_TO")
	assert.NotContains(t, md, "**Shared concepts:**")
}

// --- formatPattern tests ---

func TestFormatPattern_EnrichedWithGraph(t *testing.T) {
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
			{Name: "related-pattern-name", Relationship: "RELATED_TO", Similarity: 0.85},
			{Name: "another-related", Relationship: "RELATED_TO", Similarity: 0.72},
		},
		Concepts: []patternsvc.ConceptResult{
			{Name: "error-wrapping", Type: "practice"},
			{Name: "sentinel-errors", Type: "practice"},
		},
	}

	md := formatPattern(pattern, graphCtx)

	assert.Contains(t, md, "## go-error-handling")
	assert.Contains(t, md, "**ID:** "+patternID.String())
	assert.Contains(t, md, "**Tags:** go, errors, best-practices")
	assert.Contains(t, md, "**Enrichment:** enriched (2025-01-10T08:05:00Z)")
	assert.Contains(t, md, "## Content")
	assert.Contains(t, md, "Full pattern content here")
	assert.Contains(t, md, "### Related Patterns")
	assert.Contains(t, md, "- **related-pattern-name** (RELATED_TO, similarity: 0.85)")
	assert.Contains(t, md, "- **another-related** (RELATED_TO, similarity: 0.72)")
	assert.Contains(t, md, "### Extracted Concepts")
	assert.Contains(t, md, "- **error-wrapping**")
	assert.Contains(t, md, "- **sentinel-errors**")
}

func TestFormatPattern_PendingEnrichment(t *testing.T) {
	t.Parallel()

	pattern := &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             "pending-pattern",
		Content:          "Content here",
		EnrichmentStatus: "pending",
	}

	md := formatPattern(pattern, nil)

	assert.Contains(t, md, "**Enrichment:** pending")
	assert.NotContains(t, md, "### Related Patterns")
	assert.NotContains(t, md, "### Extracted Concepts")
}

func TestFormatPattern_FailedEnrichment(t *testing.T) {
	t.Parallel()

	errMsg := "embedding generation timed out after 30s"
	pattern := &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             "failed-pattern",
		Tags:             []string{"go", "errors"},
		Content:          "Content here",
		EnrichmentStatus: "failed",
		EnrichmentError:  &errMsg,
	}

	md := formatPattern(pattern, nil)

	assert.Contains(t, md, "**Enrichment:** failed -- embedding generation timed out after 30s")
	assert.NotContains(t, md, "### Related Patterns")
	assert.NotContains(t, md, "### Extracted Concepts")
}

func TestFormatPattern_EnrichedNoGraphSections(t *testing.T) {
	t.Parallel()

	enrichedAt := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	pattern := &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             "enriched-no-graph",
		Content:          "Content here",
		EnrichmentStatus: "enriched",
		EnrichedAt:       &enrichedAt,
	}
	// Graph context with empty slices.
	graphCtx := &patternsvc.GraphContext{
		RelatedPatterns: []patternsvc.RelatedPatternResult{},
		Concepts:        []patternsvc.ConceptResult{},
	}

	md := formatPattern(pattern, graphCtx)

	assert.Contains(t, md, "**Enrichment:** enriched")
	assert.NotContains(t, md, "### Related Patterns")
	assert.NotContains(t, md, "### Extracted Concepts")
}

func TestFormatPattern_NoTags(t *testing.T) {
	t.Parallel()

	pattern := &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             "no-tags",
		Content:          "Content",
		EnrichmentStatus: "pending",
	}

	md := formatPattern(pattern, nil)

	assert.NotContains(t, md, "**Tags:**")
}

func TestFormatPattern_FailedNoErrorMessage(t *testing.T) {
	t.Parallel()

	pattern := &patternrepo.Pattern{
		ID:               uuid.New(),
		Name:             "failed-no-msg",
		Content:          "Content",
		EnrichmentStatus: "failed",
		EnrichmentError:  nil,
	}

	md := formatPattern(pattern, nil)

	assert.Contains(t, md, "**Enrichment:** failed -- unknown error")
}
