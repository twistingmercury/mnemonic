package enrichmentjob

// JobStatus represents the valid job status values.
type JobStatus string

const (
	// StatusPending indicates the job is awaiting processing.
	StatusPending JobStatus = "pending"
)

// DefaultMaxAttempts is the default maximum number of attempts for a job.
const DefaultMaxAttempts = 3
