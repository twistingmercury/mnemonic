package chunk

import (
	"time"

	"github.com/google/uuid"
)

// Chunk is one H2-bounded section of a parent pattern.
// It maps to a row in the pattern_chunks table.
type Chunk struct {
	// ID is the unique identifier for the chunk.
	ID uuid.UUID

	// PatternID references the parent pattern (patterns.id).
	PatternID uuid.UUID

	// SectionTitle is the H2 heading text (e.g., "Philosophy", "created_at Handling").
	SectionTitle string

	// ChunkIndex is the zero-based position of this chunk within the parent pattern.
	ChunkIndex int

	// Content is the raw markdown text of the section (heading not included).
	Content string

	// EnrichmentStatus is one of "pending", "enriched", or "failed".
	EnrichmentStatus string

	// EnrichmentError holds the error message when EnrichmentStatus is "failed".
	EnrichmentError *string

	// EnrichedAt is set when EnrichmentStatus transitions to "enriched".
	EnrichedAt *time.Time

	// CreatedAt is the row creation timestamp.
	CreatedAt time.Time

	// UpdatedAt is the last modification timestamp.
	UpdatedAt time.Time
}

// Match is a similarity search result: chunk content plus parent pattern metadata.
type Match struct {
	// ChunkID is the ID of the matching chunk.
	ChunkID uuid.UUID

	// PatternID is the ID of the parent pattern.
	PatternID uuid.UUID

	// PatternName is the name of the parent pattern (patterns.name).
	PatternName string

	// EntityType is the pattern's entity type (e.g., "go-pattern").
	EntityType string

	// Language is the primary language of the pattern (e.g., "go", "agnostic").
	Language string

	// Domain is the technical domain (e.g., "backend", "testing").
	Domain string

	// Tags are the parent pattern's tags.
	Tags []string

	// SectionTitle is the H2 heading of the matching chunk.
	SectionTitle string

	// ChunkIndex is the zero-based position of the chunk within its parent pattern.
	ChunkIndex int

	// Content is the text content of the matching chunk.
	Content string

	// Similarity is the cosine similarity score in [0, 1] — higher is more similar.
	Similarity float64
}

// SimilarityOptions controls the behaviour of FindSimilar.
type SimilarityOptions struct {
	// MinSimilarity is the minimum cosine similarity score to include in results.
	MinSimilarity float64

	// MaxResults caps the number of results returned.
	MaxResults int

	// Language filters results to chunks whose parent pattern has this language.
	// Empty string disables this filter.
	Language string

	// Domain filters results to chunks whose parent pattern has this domain.
	// Empty string disables this filter.
	Domain string

	// Tags restricts results to patterns whose tags contain all of these values.
	// Empty slice disables this filter.
	Tags []string
}
