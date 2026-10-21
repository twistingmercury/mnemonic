package chunk

import "github.com/google/uuid"

// Match is a similarity search result: chunk content plus the parent pattern
// metadata search_patterns shows.
type Match struct {
	// PatternID is the ID of the parent pattern.
	PatternID uuid.UUID

	// PatternName is the name of the parent pattern (patterns.name).
	PatternName string

	// Tags are the parent pattern's tags.
	Tags []string

	// SectionTitle is the H2 heading of the matching chunk.
	SectionTitle string

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
