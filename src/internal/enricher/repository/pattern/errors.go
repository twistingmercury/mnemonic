package pattern

import "errors"

// ErrNotFound is returned when a pattern with the specified ID cannot be found.
var ErrNotFound = errors.New("pattern not found")
