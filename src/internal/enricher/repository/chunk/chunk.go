package chunk

import "github.com/google/uuid"

// Chunk is one H2-bounded section of a parent pattern, with the parent
// pattern fields its embedding text needs.
type Chunk struct {
	// ID is the unique identifier for the chunk.
	ID uuid.UUID

	// PatternID references the parent pattern (patterns.id).
	PatternID uuid.UUID

	// SectionTitle is the H2 heading text (e.g., "Philosophy").
	SectionTitle string

	// Content is the raw markdown text of the section (heading not included).
	Content string

	// PatternName is the parent pattern's name.
	PatternName string

	// PatternTags are the parent pattern's tags.
	PatternTags []string
}

// Progress summarizes the enrichment state of all chunks of one pattern.
type Progress struct {
	// AnyFailed is true when at least one chunk has status "failed".
	AnyFailed bool

	// AllEnriched is true when the pattern has chunks and every one has
	// status "enriched".
	AllEnriched bool
}
