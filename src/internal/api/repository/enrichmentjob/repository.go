package enrichmentjob

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/twistingmercury/librarian-api/internal/repository"
)

// Repository defines data access operations for enrichment jobs.
type Repository interface {
	// CreatePendingForChunks stores one pending job per chunk in a single
	// statement and returns the new job IDs in the same order as chunkIDs.
	CreatePendingForChunks(ctx context.Context, chunkIDs []uuid.UUID) ([]uuid.UUID, error)

	// WithTx returns a Repository that executes all operations within db.
	// Use this to enlist the repository in a caller-owned transaction.
	WithTx(db repository.DBTX) Repository
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// CreatePendingForChunks inserts one pending job per chunk with a single
// INSERT ... SELECT FROM unnest, so the cost is one round trip regardless of
// how many chunks a pattern has. Job IDs are generated here so the caller gets
// them back in chunk order without depending on RETURNING row order.
func (r *pgxRepository) CreatePendingForChunks(ctx context.Context, chunkIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(chunkIDs) == 0 {
		return []uuid.UUID{}, nil
	}

	jobIDs := make([]uuid.UUID, len(chunkIDs))
	for i := range jobIDs {
		jobIDs[i] = uuid.New()
	}

	query := `
		INSERT INTO enrichment_jobs (
			id, chunk_id, status, attempts, max_attempts,
			scheduled_for, created_at, updated_at
		)
		SELECT u.id, u.chunk_id, $3, 0, $4, now(), now(), now()
		FROM unnest($1::uuid[], $2::uuid[]) AS u(id, chunk_id)
	`

	if _, err := r.db.Exec(ctx, query, jobIDs, chunkIDs, string(StatusPending), DefaultMaxAttempts); err != nil {
		return nil, fmt.Errorf("create enrichment jobs: %w", err)
	}

	return jobIDs, nil
}

// WithTx returns a Repository that executes all operations within db.
func (r *pgxRepository) WithTx(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}
