//go:build integration

// Package pattern_test's integration tests drive the real service against a
// real PostgreSQL. They cover the two things mocks and the black-box E2E suite
// cannot reach: that a rollback genuinely discards rows, and states that no
// public endpoint can produce.
package pattern_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/librarian-api/internal/queue"
	chunkrepo "github.com/twistingmercury/librarian-api/internal/repository/chunk"
	enrichmentrepo "github.com/twistingmercury/librarian-api/internal/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/librarian-api/internal/repository/graph"
	patternrepo "github.com/twistingmercury/librarian-api/internal/repository/pattern"
	patternsvc "github.com/twistingmercury/librarian-api/internal/service/pattern"
)

const integrationContent = "[//]: pattern\n## Philosophy\nStorage-only databases.\n\n[//]: pattern\n## Usage\nCall the API."

// recordingPublisher stands in for RabbitMQ. Publication is best-effort and
// happens after commit, so these tests only need to observe whether it ran.
type recordingPublisher struct{ published []uuid.UUID }

func (p *recordingPublisher) Publish(_ context.Context, jobID uuid.UUID) error {
	p.published = append(p.published, jobID)
	return nil
}
func (p *recordingPublisher) Close() error { return nil }

var _ queue.Publisher = (*recordingPublisher)(nil)

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	connStr := os.Getenv("TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("skipping integration test: TEST_DATABASE_URL not set (run make tests-integration)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("skipping integration test: unable to connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: ping failed: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// newIntegrationService wires the real repositories against pool. graphRepo is
// nil: Create and Update never touch Neo4j, and a test that starts doing so
// should fail loudly here rather than silently exercise a stub.
func newIntegrationService(t *testing.T, pool *pgxpool.Pool) (patternsvc.Service, *recordingPublisher) {
	t.Helper()
	pub := &recordingPublisher{}
	svc := patternsvc.New(
		patternrepo.NewRepository(pool),
		enrichmentrepo.NewRepository(pool),
		graphrepo.Repository(nil),
		pool,
		chunkrepo.NewRepository(pool),
		pub,
		zerolog.Nop(),
	)
	return svc, pub
}

func uniqueName(t *testing.T) string {
	t.Helper()
	return "inttest-" + uuid.NewString()
}

// countsFor reports the rows a pattern owns, so a rollback can be shown to have
// left nothing behind rather than merely returning an error.
func countsFor(t *testing.T, pool *pgxpool.Pool, name string) (patterns, chunks, jobs int) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from patterns where name = $1`, name).Scan(&patterns))
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from pattern_chunks c join patterns p on p.id = c.pattern_id where p.name = $1`, name).Scan(&chunks))
	require.NoError(t, pool.QueryRow(ctx,
		`select count(*) from enrichment_jobs j
		   join pattern_chunks c on c.id = j.chunk_id
		   join patterns p on p.id = c.pattern_id
		 where p.name = $1`, name).Scan(&jobs))
	return patterns, chunks, jobs
}

// TestIntegration_CreateIsAtomic proves the rollback in the engine, not in a
// mock: a chunk failure must leave no pattern, no chunks and no jobs, and the
// name must still be free. Mocks can only show that Rollback was called.
func TestIntegration_CreateIsAtomic(t *testing.T) {
	pool := integrationPool(t)
	svc, pub := newIntegrationService(t, pool)
	ctx := context.Background()
	name := uniqueName(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from patterns where name = $1`, name)
	})

	// A section title longer than pattern_chunks.section_title (varchar(255))
	// fails the chunk INSERT after the pattern row is already in the
	// transaction.
	overlong := "[//]: pattern\n## " + strings.Repeat("T", 300) + "\nBody."

	_, err := svc.Create(ctx, patternsvc.Input{Name: name, Content: overlong})
	require.Error(t, err, "an over-long section title must fail the create")

	patterns, chunks, jobs := countsFor(t, pool, name)
	assert.Zero(t, patterns, "the pattern row must not survive a failed create")
	assert.Zero(t, chunks, "no chunk may survive a failed create")
	assert.Zero(t, jobs, "no enrichment job may survive a failed create")
	assert.Empty(t, pub.published, "nothing may be published for a rolled-back create")

	// The name is free, so the caller's retry is not a dead end.
	created, err := svc.Create(ctx, patternsvc.Input{Name: name, Content: integrationContent})
	require.NoError(t, err, "retry after rollback must succeed")
	require.NotNil(t, created)

	patterns, chunks, jobs = countsFor(t, pool, name)
	assert.Equal(t, 1, patterns)
	assert.Equal(t, 2, chunks, "both chunks are written in the same transaction")
	assert.Equal(t, 2, jobs, "every chunk gets its enrichment job")
	assert.Len(t, pub.published, 2, "both jobs publish after the commit")
}

// TestIntegration_UpdateResetsEnrichedParent covers the one Task 3 criterion
// that neither mocks nor the black-box suite can reach. An enriched pattern
// with a populated enriched_at only exists after the enricher has run, and no
// endpoint can put a pattern into that state, so the rows are seeded directly.
//
// The reset matters because Update deletes the old chunks — cascading their
// jobs away — and writes new pending ones. A pattern left marked "enriched"
// with an old enriched_at would advertise embeddings that no longer exist.
func TestIntegration_UpdateResetsEnrichedParent(t *testing.T) {
	pool := integrationPool(t)
	svc, pub := newIntegrationService(t, pool)
	ctx := context.Background()

	for _, seeded := range []struct {
		name     string
		status   string
		errMsg   *string
		enriched bool
	}{
		{name: "enriched parent", status: "enriched", enriched: true},
		{name: "failed parent", status: "failed", errMsg: new("embedding provider rejected the request")},
	} {
		t.Run(seeded.name, func(t *testing.T) {
			patternName := uniqueName(t)
			t.Cleanup(func() {
				_, _ = pool.Exec(ctx, `delete from patterns where name = $1`, patternName)
			})

			created, err := svc.Create(ctx, patternsvc.Input{Name: patternName, Content: integrationContent})
			require.NoError(t, err)

			// Seed the terminal state the enricher would have produced,
			// backdated so a reset is distinguishable from a no-op.
			staleTime := time.Now().Add(-72 * time.Hour).UTC()
			_, err = pool.Exec(ctx, `
				update patterns
				   set enrichment_status = $2, enrichment_error = $3, enriched_at = $4
				 where id = $1`, created.ID, seeded.status, seeded.errMsg, staleTime)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `
				update pattern_chunks
				   set enrichment_status = $2, enriched_at = $3, embedding = null
				 where pattern_id = $1`, created.ID, seeded.status, staleTime)
			require.NoError(t, err)

			pub.published = nil

			_, err = svc.Update(ctx, created.ID, patternsvc.Input{
				Name:    patternName,
				Content: "[//]: pattern\n## Rationale\nWhy it exists.",
			})
			require.NoError(t, err)

			var status string
			var enrichmentError *string
			var enrichedAt *time.Time
			require.NoError(t, pool.QueryRow(ctx,
				`select enrichment_status, enrichment_error, enriched_at from patterns where id = $1`,
				created.ID).Scan(&status, &enrichmentError, &enrichedAt))

			assert.Equal(t, "pending", status, "an updated pattern awaits re-enrichment")
			assert.Nil(t, enrichmentError, "the previous failure must not outlive the content it described")
			assert.Nil(t, enrichedAt, "a stale enriched_at would claim embeddings that were just deleted")

			// The old chunks and their jobs are gone, replaced by one pending
			// chunk with its own job.
			patterns, chunks, jobs := countsFor(t, pool, patternName)
			assert.Equal(t, 1, patterns)
			assert.Equal(t, 1, chunks, "the two original chunks are replaced by one")
			assert.Equal(t, 1, jobs)
			assert.Len(t, pub.published, 1, "the replacement job is published after commit")

			var chunkStatus string
			var chunkTitle string
			require.NoError(t, pool.QueryRow(ctx,
				`select pattern_chunks.enrichment_status, pattern_chunks.section_title
				   from pattern_chunks
				   join patterns on patterns.id = pattern_chunks.pattern_id
				  where patterns.name = $1`, patternName).Scan(&chunkStatus, &chunkTitle))
			assert.Equal(t, "pending", chunkStatus)
			assert.Equal(t, "Rationale", chunkTitle)
		})
	}
}

// TestIntegration_CreateBatchesChunksAndJobs checks the batched inserts against
// the real schema: chunks land in section order with DB defaults filled in,
// each gets exactly one pending job, and the published IDs are those jobs.
func TestIntegration_CreateBatchesChunksAndJobs(t *testing.T) {
	pool := integrationPool(t)
	svc, pub := newIntegrationService(t, pool)
	ctx := context.Background()
	name := uniqueName(t)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `delete from patterns where name = $1`, name)
	})

	content := integrationContent + "\n\n[//]: pattern\n## Limits\nNone."
	created, err := svc.Create(ctx, patternsvc.Input{Name: name, Content: content})
	require.NoError(t, err)

	rows, err := pool.Query(ctx, `
		select c.chunk_index, c.section_title, c.enrichment_status, j.id, j.status, j.attempts, j.max_attempts
		  from pattern_chunks c
		  join enrichment_jobs j on j.chunk_id = c.id
		 where c.pattern_id = $1
		 order by c.chunk_index`, created.ID)
	require.NoError(t, err)
	defer rows.Close()

	var titles []string
	var jobIDs []uuid.UUID
	for rows.Next() {
		var idx, attempts, maxAttempts int
		var title, chunkStatus, jobStatus string
		var jobID uuid.UUID
		require.NoError(t, rows.Scan(&idx, &title, &chunkStatus, &jobID, &jobStatus, &attempts, &maxAttempts))
		assert.Equal(t, len(titles), idx)
		assert.Equal(t, "pending", chunkStatus)
		assert.Equal(t, "pending", jobStatus)
		assert.Zero(t, attempts)
		assert.Equal(t, enrichmentrepo.DefaultMaxAttempts, maxAttempts)
		titles = append(titles, title)
		jobIDs = append(jobIDs, jobID)
	}
	require.NoError(t, rows.Err())

	assert.Equal(t, []string{"Philosophy", "Usage", "Limits"}, titles)
	assert.Equal(t, jobIDs, pub.published, "published job IDs are the committed rows, in chunk order")

	summaries, err := svc.ListChunks(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, summaries, 3)
	for _, c := range summaries {
		assert.Empty(t, c.Content, "summaries never load content")
	}
}
