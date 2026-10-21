//go:build integration

package pattern_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-mcp/internal/repository/pattern"
)

// testPatternPrefix is used to identify test-created patterns for cleanup.
const testPatternPrefix = "test-integration-pattern-"

// setupTestDB connects to the test database, skipping the test if it is
// unavailable, and removes test patterns before and after the test.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	connStr := os.Getenv("TEST_DATABASE_URL")
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
		query := fmt.Sprintf("DELETE FROM patterns WHERE name LIKE '%s%%'", testPatternPrefix)
		if _, err := pool.Exec(context.Background(), query); err != nil {
			t.Logf("warning: failed to cleanup test patterns: %v", err)
		}
	}
	cleanup()
	t.Cleanup(func() {
		cleanup()
		pool.Close()
	})

	return pool
}

// insertPattern seeds one pattern row the way mnemonic-api writes it.
func insertPattern(t *testing.T, pool *pgxpool.Pool, suffix, language string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO patterns (
			id, name, content, tags, entity_type, language, domain,
			related_patterns, enrichment_status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, 'go-pattern', $5, 'backend', '[]', 'pending', now(), now())`,
		id, testPatternPrefix+suffix, "content for "+suffix, `["test","integration"]`, language,
	)
	require.NoError(t, err)
	return id
}

func TestIntegration_Get(t *testing.T) {
	pool := setupTestDB(t)
	repo := pattern.NewRepository(pool)
	ctx := context.Background()
	id := insertPattern(t, pool, "get", "go")

	t.Run("retrieves existing pattern by ID", func(t *testing.T) {
		p, err := repo.Get(ctx, id)

		require.NoError(t, err)
		assert.Equal(t, id, p.ID)
		assert.Equal(t, testPatternPrefix+"get", p.Name)
		assert.Equal(t, "content for get", p.Content)
		assert.Equal(t, []string{"test", "integration"}, p.Tags)
		assert.Equal(t, "pending", p.EnrichmentStatus)
	})

	t.Run("returns ErrNotFound for nonexistent ID", func(t *testing.T) {
		p, err := repo.Get(ctx, uuid.New())

		assert.ErrorIs(t, err, pattern.ErrNotFound)
		assert.Nil(t, p)
	})

	t.Run("respects cancelled context", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := repo.Get(cancelled, id)
		assert.Error(t, err)
	})
}

func TestIntegration_GetName(t *testing.T) {
	pool := setupTestDB(t)
	repo := pattern.NewRepository(pool)
	ctx := context.Background()
	id := insertPattern(t, pool, "name", "go")

	name, err := repo.GetName(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, testPatternPrefix+"name", name)

	_, err = repo.GetName(ctx, uuid.New())
	assert.ErrorIs(t, err, pattern.ErrNotFound)
}

func TestIntegration_GetByIDs(t *testing.T) {
	pool := setupTestDB(t)
	repo := pattern.NewRepository(pool)
	ctx := context.Background()
	id1 := insertPattern(t, pool, "ids-1", "go")
	id2 := insertPattern(t, pool, "ids-2", "python")

	patterns, err := repo.GetByIDs(ctx, []uuid.UUID{id1, id2, uuid.New()})
	require.NoError(t, err)
	require.Len(t, patterns, 2)

	byID := map[uuid.UUID]*pattern.Pattern{}
	for _, p := range patterns {
		byID[p.ID] = p
	}
	assert.Equal(t, "go", byID[id1].Language)
	assert.Equal(t, "python", byID[id2].Language)
	assert.Equal(t, "backend", byID[id2].Domain)
	assert.Equal(t, []string{"test", "integration"}, byID[id2].Tags)
	assert.Empty(t, byID[id1].Content, "GetByIDs must not load content")
}
