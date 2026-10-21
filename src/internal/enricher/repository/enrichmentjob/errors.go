package enrichmentjob

import "errors"

var (
	// ErrNotFound is returned when an enrichment job with the specified ID cannot be found.
	ErrNotFound = errors.New("enrichment job not found")

	// ErrUnavailable is returned by Start when the job does not exist, is not
	// pending, or is not yet due.
	ErrUnavailable = errors.New("enrichment job not found, already started, or not yet due")
)
