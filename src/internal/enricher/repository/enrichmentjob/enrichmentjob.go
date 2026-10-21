package enrichmentjob

import "github.com/google/uuid"

// Job identifies an enrichment job and what it enriches. Exactly one of
// PatternID and ChunkID is set.
type Job struct {
	// ID is the unique identifier for the job.
	ID uuid.UUID

	// PatternID is set for legacy pattern-level jobs.
	PatternID *uuid.UUID

	// ChunkID is set for chunk-level jobs.
	ChunkID *uuid.UUID
}

// JobStatus represents the current state of an enrichment job.
type JobStatus string

// Job statuses stored in enrichment_jobs.status.
const (
	StatusPending    JobStatus = "pending"
	StatusProcessing JobStatus = "processing"
	StatusCompleted  JobStatus = "completed"
	StatusFailed     JobStatus = "failed"
)
