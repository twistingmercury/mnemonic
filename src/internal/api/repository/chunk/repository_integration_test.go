//go:build integration

package chunk_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/mnemonic/internal/api/repository/chunk"
)

const (
	testPatternPrefix = "test-integration-api-chunk-"

	// embeddingDims is the width of pattern_chunks.embedding (migration 000010).
	embeddingDims = 2000
)

// setupTestDB connects to the test database, skipping the test if it is not
// configured, and removes test patterns (and their chunks) before and after.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := os.Getenv("MNEMONIC_TEST_DATABASE_URL")
	if connStr == "" {
		t.Skip("skipping integration test: MNEMONIC_TEST_DATABASE_URL not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Skipf("skipping integration test: unable to connect to database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: database ping failed: %v", err)
	}

	cleanup := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM patterns WHERE name LIKE $1`, testPatternPrefix+"%")
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		pool.Close()
	})

	return pool
}

// unitVector returns an embeddingDims-wide vector with 1 at axis and 0 elsewhere,
// so vectors on the same axis have similarity 1 and on different axes 0.
func unitVector(axis int) []float32 {
	v := make([]float32, embeddingDims)
	v[axis] = 1

	return v
}

// seedChunk inserts a pattern and one chunk carrying embedding. The statuses
// are the values the enricher writes: a pattern and its chunks start 'pending'
// and become 'enriched' or 'failed' (migration 000009 constrains the set).
func seedChunk(t *testing.T, pool *pgxpool.Pool, suffix, status string, embedding []float32) (patternID, chunkID uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	patternID = uuid.New()
	chunkID = uuid.New()

	_, err := pool.Exec(ctx, `
		INSERT INTO patterns (id, name, content, tags, entity_type, language, domain, enrichment_status)
		VALUES ($1, $2, 'content', '[]', 'go-pattern', 'go', 'backend', $3)`,
		patternID, testPatternPrefix+suffix, status)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pattern_chunks (id, pattern_id, section_title, chunk_index, content, embedding, enrichment_status)
		VALUES ($1, $2, 'Overview', 0, $3, $4, $5)`,
		chunkID, patternID, "chunk of "+suffix, pgvector.NewVector(embedding), status)
	require.NoError(t, err)

	return patternID, chunkID
}

// TestIntegration_FindSimilar_OnlyEnrichedChunks seeds identical embeddings
// under enriched, pending and failed chunks. Similarity alone would return all
// three; only the status predicate can drop two, which a mock returning no rows
// cannot prove.
func TestIntegration_FindSimilar_OnlyEnrichedChunks(t *testing.T) {
	pool := setupTestDB(t)
	repo := chunk.NewRepository(pool)
	ctx := context.Background()

	embedding := unitVector(0)
	enrichedPattern, enrichedChunk := seedChunk(t, pool, "enriched", "enriched", embedding)
	seedChunk(t, pool, "pending", "pending", embedding)
	seedChunk(t, pool, "failed", "failed", embedding)

	matches, err := repo.FindSimilar(ctx, embedding, chunk.SimilarityOptions{MinSimilarity: 0.5, MaxResults: 10})
	require.NoError(t, err)

	require.Len(t, matches, 1, "only the enriched chunk may be searchable")
	assert.Equal(t, enrichedChunk, matches[0].ChunkID)
	assert.Equal(t, enrichedPattern, matches[0].PatternID)
	assert.Equal(t, testPatternPrefix+"enriched", matches[0].PatternName)
	assert.InDelta(t, 1.0, matches[0].Similarity, 1e-6)
}
