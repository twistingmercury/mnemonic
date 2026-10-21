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

	"github.com/twistingmercury/mnemonic/internal/mcp/repository/chunk"
)

const testPatternPrefix = "test-integration-chunk-"

// setupTestDB connects to the test database, skipping the test if it is
// unavailable, and removes test patterns (and their chunks) before and after.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := os.Getenv("MNEMONIC_TEST_DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://mnemonic:mnemonic_dev@localhost:15433/mnemonic?sslmode=disable"
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

// insertPatternWithChunk seeds a pattern and one chunk with the given embedding.
func insertPatternWithChunk(t *testing.T, pool *pgxpool.Pool, suffix, language, status string, embedding []float32) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	id := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO patterns (id, name, content, tags, entity_type, language, domain,
			related_patterns, enrichment_status, created_at, updated_at)
		VALUES ($1, $2, 'content', '["go","testing"]', 'go-pattern', $3, 'backend', '[]', 'enriched', now(), now())`,
		id, testPatternPrefix+suffix, language)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO pattern_chunks (id, pattern_id, section_title, chunk_index, content, embedding, enrichment_status)
		VALUES ($1, $2, 'Overview', 0, $3, $4, $5)`,
		uuid.New(), id, "chunk of "+suffix, pgvector.NewVector(embedding), status)
	require.NoError(t, err)
	return id
}

func TestIntegration_FindSimilar(t *testing.T) {
	pool := setupTestDB(t)
	repo := chunk.NewRepository(pool)
	ctx := context.Background()

	close1 := insertPatternWithChunk(t, pool, "close", "go", "enriched", []float32{1, 0, 0})
	close2 := insertPatternWithChunk(t, pool, "near", "python", "enriched", []float32{0.9, 0.1, 0})
	insertPatternWithChunk(t, pool, "far", "go", "enriched", []float32{0, 1, 0})
	insertPatternWithChunk(t, pool, "pending", "go", "pending", []float32{1, 0, 0})

	query := []float32{1, 0, 0}

	t.Run("orders by similarity and applies threshold and status", func(t *testing.T) {
		matches, err := repo.FindSimilar(ctx, query, chunk.SimilarityOptions{MinSimilarity: 0.5, MaxResults: 10})
		require.NoError(t, err)
		require.Len(t, matches, 2)
		assert.Equal(t, close1, matches[0].PatternID)
		assert.Equal(t, testPatternPrefix+"close", matches[0].PatternName)
		assert.Equal(t, []string{"go", "testing"}, matches[0].Tags)
		assert.Equal(t, "Overview", matches[0].SectionTitle)
		assert.Equal(t, "chunk of close", matches[0].Content)
		assert.InDelta(t, 1.0, matches[0].Similarity, 1e-6)
		assert.Equal(t, close2, matches[1].PatternID)
	})

	t.Run("filters by language and tags", func(t *testing.T) {
		matches, err := repo.FindSimilar(ctx, query, chunk.SimilarityOptions{
			MinSimilarity: 0.5, MaxResults: 10, Language: "python", Tags: []string{"go"},
		})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, close2, matches[0].PatternID)

		matches, err = repo.FindSimilar(ctx, query, chunk.SimilarityOptions{
			MinSimilarity: 0.5, MaxResults: 10, Tags: []string{"rust"},
		})
		require.NoError(t, err)
		assert.Empty(t, matches)
	})

	t.Run("respects limit", func(t *testing.T) {
		matches, err := repo.FindSimilar(ctx, query, chunk.SimilarityOptions{MinSimilarity: 0, MaxResults: 1})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, close1, matches[0].PatternID)
	})
}
