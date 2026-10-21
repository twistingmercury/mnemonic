package pattern

import "github.com/google/uuid"

// Pattern holds the pattern fields concept extraction and graph sync need.
type Pattern struct {
	// ID is the unique identifier for the pattern.
	ID uuid.UUID

	// Name is the human-readable name of the pattern.
	Name string

	// Description is an optional description of the pattern's purpose.
	Description *string

	// Content is the full markdown content of the pattern.
	Content string
}
