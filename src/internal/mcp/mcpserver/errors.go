package mcpserver

import "errors"

// ErrPatternNotFound is returned when a pattern ID does not exist.
// Applies to: get_pattern, find_related_patterns.
var ErrPatternNotFound = errors.New("pattern not found")

// ErrInvalidInput is returned when handler-level validation fails, for
// constraints the SDK's schema validation does not catch: a pattern ID that
// is not a UUID. Out-of-range limits and thresholds fail the input schema.
// Applies to: get_pattern, find_related_patterns.
var ErrInvalidInput = errors.New("invalid input")

// ErrServiceUnavailable is returned when a backend database
// (Postgres, Neo4j) is unreachable.
// Applies to: all 3 tools.
var ErrServiceUnavailable = errors.New("service unavailable")
