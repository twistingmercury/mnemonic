package helpers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultPostgresDSN = "postgres://mnemonic:mnemonic_dev@localhost:5432/mnemonic?sslmode=disable"

// NewPGConn creates a pgxpool.Pool from the POSTGRES_DSN env var.
// Falls back to the default local DSN if the env var is not set.
// Calls t.Fatal on connection error.
func NewPGConn(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := envOr("POSTGRES_DSN", defaultPostgresDSN)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create postgres pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping postgres: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// SeedPattern inserts a minimal pattern row and returns its ID.
// Uses a unique name to avoid conflicts. enrichment_status is set to "pending".
func SeedPattern(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	id := uuid.New()
	name := fmt.Sprintf("test-pattern-%s", uuid.New())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, `
		INSERT INTO patterns (
			id, name, description, content, tags, entity_type, language, domain,
			version, related_patterns, enrichment_status, created_at, updated_at
		) VALUES (
			$1, $2, 'Test pattern description', 'Test pattern content',
			'[]'::jsonb, 'pattern', 'go', 'testing',
			'1.0.0', '[]'::jsonb, 'pending',
			NOW(), NOW()
		)`,
		id, name,
	)
	if err != nil {
		t.Fatalf("failed to seed pattern: %v", err)
	}

	return id
}

// SeedChunk inserts a chunk row linked to patternID and returns its ID.
func SeedChunk(t *testing.T, pool *pgxpool.Pool, patternID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, `
		INSERT INTO pattern_chunks (
			id, pattern_id, section_title, chunk_index, content,
			enrichment_status, created_at, updated_at
		) VALUES (
			$1, $2, 'Test Section', 0, 'Test chunk content for enrichment.',
			'pending', NOW(), NOW()
		)`,
		id, patternID,
	)
	if err != nil {
		t.Fatalf("failed to seed chunk: %v", err)
	}

	return id
}

// SeedEnrichmentJob inserts a chunk-based enrichment_job row with status="pending".
// chunk_id is set; pattern_id is left NULL per the enrichment_jobs_target_exclusive
// check constraint (each job targets exactly one of: chunk or pattern).
// Returns the job ID.
func SeedEnrichmentJob(t *testing.T, pool *pgxpool.Pool, chunkID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, `
		INSERT INTO enrichment_jobs (
			id, chunk_id, status, attempts, max_attempts,
			scheduled_for, created_at, updated_at
		) VALUES (
			$1, $2, 'pending', 0, 1,
			NOW(), NOW(), NOW()
		)`,
		id, chunkID,
	)
	if err != nil {
		t.Fatalf("failed to seed enrichment job: %v", err)
	}

	return id
}

// CleanupPattern deletes the pattern by ID. FK cascades handle chunks and jobs.
func CleanupPattern(t *testing.T, pool *pgxpool.Pool, patternID uuid.UUID) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx,
		`DELETE FROM patterns WHERE id = $1`,
		patternID,
	)
	if err != nil {
		t.Fatalf("failed to cleanup pattern %s: %v", patternID, err)
	}
}

// PollJobStatusRaw polls enrichment_jobs every 500ms until status is "completed" or "failed"
// or timeout expires. Returns (status, lastError). Does NOT call t.Fatal on "failed".
// Calls t.Fatal only on timeout or query error.
func PollJobStatusRaw(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, timeout time.Duration) (string, string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for job %s to complete after %v", jobID, timeout)
			return "", ""
		default:
		}

		var status string
		var lastError *string
		err := pool.QueryRow(ctx, `SELECT status, last_error FROM enrichment_jobs WHERE id = $1`, jobID).
			Scan(&status, &lastError)
		if err != nil {
			t.Fatalf("failed to query job status for %s: %v", jobID, err)
		}

		if status == "completed" || status == "failed" {
			errMsg := ""
			if lastError != nil {
				errMsg = *lastError
			}
			return status, errMsg
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// PollJobStatus is PollJobStatusRaw, but also calls t.Fatal if the job fails.
func PollJobStatus(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, timeout time.Duration) string {
	t.Helper()
	status, lastError := PollJobStatusRaw(t, pool, jobID, timeout)
	if status == "failed" {
		t.Fatalf("job %s failed: last_error=%s", jobID, lastError)
	}
	return status
}

// AssertChunkEmbeddingSet asserts that the chunk's embedding column is NOT NULL.
func AssertChunkEmbeddingSet(t *testing.T, pool *pgxpool.Pool, chunkID uuid.UUID) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var embeddingSet bool
	err := pool.QueryRow(ctx,
		`SELECT embedding IS NOT NULL FROM pattern_chunks WHERE id = $1`,
		chunkID,
	).Scan(&embeddingSet)
	if err != nil {
		t.Fatalf("failed to query embedding for chunk %s: %v", chunkID, err)
	}

	if !embeddingSet {
		t.Fatalf("expected embedding to be set for chunk %s, but it was NULL", chunkID)
	}
}
