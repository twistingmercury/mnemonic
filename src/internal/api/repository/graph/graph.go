package graph

import "github.com/google/uuid"

// Concept represents a concept node linked to patterns via MENTIONED_IN relationships.
type Concept struct {
	// Name is the normalized lowercase name of the concept.
	Name string
}

// RelatedPattern represents a pattern discovered through shared concepts.
type RelatedPattern struct {
	// ID is the unique identifier of the related pattern.
	ID uuid.UUID

	// Name is the human-readable name of the related pattern.
	Name string

	// SharedConcepts is the number of concepts shared between the source and this pattern.
	SharedConcepts int

	// Similarity is the pre-computed similarity score from the RELATED_TO edge (0.0-1.0).
	// Computed as: sharedConcepts / max(totalConceptsA, totalConceptsB).
	Similarity float64

	// ConceptNames contains the names of the shared concepts between the two patterns.
	ConceptNames []string
}

// PatternGraph is a pattern's neighbourhood in the knowledge graph.
type PatternGraph struct {
	RelatedPatterns []RelatedPattern
	Concepts        []Concept
}
