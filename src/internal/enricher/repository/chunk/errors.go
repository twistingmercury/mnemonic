package chunk

import "errors"

var (
	// ErrNotFound is returned when a chunk does not exist.
	ErrNotFound = errors.New("chunk not found")
)
