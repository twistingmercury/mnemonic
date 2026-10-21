package graph

import "github.com/google/uuid"

// Pattern represents a pattern node in the Neo4j knowledge graph.
type Pattern struct {
	// ID is the unique identifier for the pattern, matching the PostgreSQL pattern UUID.
	ID uuid.UUID

	// Name is the human-readable name of the pattern.
	Name string

	// Description is an optional description of the pattern's purpose.
	Description *string
}

// Concept represents a concept node linked to patterns via MENTIONED_IN relationships.
type Concept struct {
	// Name is the normalized lowercase name of the concept.
	Name string

	// Type classifies the concept: technology, practice, or domain.
	Type string
}
