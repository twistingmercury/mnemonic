package pattern

import "errors"

// Common repository errors for pattern operations.
var (
	// ErrNotFound is returned when a pattern with the specified ID or name cannot be found.
	ErrNotFound = errors.New("pattern not found")

	// ErrNameExists is returned when attempting to create a pattern with a name that already exists.
	ErrNameExists = errors.New("pattern name already exists")
)
