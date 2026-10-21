//go:build integration

package pattern_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian-api/internal/repository"
	"github.com/twistingmercury/librarian-api/internal/repository/pattern"
)

const (
	// testPatternPrefix is used to identify test-created patterns for cleanup.
	testPatternPrefix = "test-integration-pattern-"
)

// setupTestDB creates a connection pool to the test database.
// It skips the test if the database is unavailable.
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

	// Verify connection is working
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: database ping failed: %v", err)
	}

	// Register cleanup to close pool when test completes
	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// cleanupTestPatterns removes all patterns with the test prefix from the database.
func cleanupTestPatterns(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := fmt.Sprintf("DELETE FROM patterns WHERE name LIKE '%s%%'", testPatternPrefix)
	_, err := pool.Exec(ctx, query)
	if err != nil {
		t.Logf("warning: failed to cleanup test patterns: %v", err)
	}
}

// testIntegrationPattern creates a sample pattern for integration testing with a unique name.
func testIntegrationPattern(suffix string) *pattern.Pattern {
	return &pattern.Pattern{
		Name:        testPatternPrefix + suffix,
		Description: new("Integration test pattern: " + suffix),
		Content:     "This is test content for integration testing. It should be stored correctly.",
		Tags:        []string{"test", "integration"},
	}
}

func TestIntegration_Create(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	t.Run("successful creation with all fields", func(t *testing.T) {
		p := testIntegrationPattern("create-full")
		err := repo.Create(ctx, p)

		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, p.ID, "ID should be set")
		assert.False(t, p.CreatedAt.IsZero(), "CreatedAt should be set")
		assert.False(t, p.UpdatedAt.IsZero(), "UpdatedAt should be set")
		assert.Equal(t, "pending", p.EnrichmentStatus, "EnrichmentStatus should be pending")

		// Verify by reading back
		retrieved, err := repo.Get(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, p.Name, retrieved.Name)
		assert.Equal(t, *p.Description, *retrieved.Description)
		assert.Equal(t, p.Content, retrieved.Content)
		assert.Equal(t, p.Tags, retrieved.Tags)
	})

	t.Run("successful creation with nil description", func(t *testing.T) {
		p := &pattern.Pattern{
			Name:    testPatternPrefix + "create-nil-desc",
			Content: "Content without description",
			Tags:    []string{"test"},
		}

		err := repo.Create(ctx, p)
		require.NoError(t, err)

		retrieved, err := repo.Get(ctx, p.ID)
		require.NoError(t, err)
		assert.Nil(t, retrieved.Description)
	})

	t.Run("successful creation with empty tags", func(t *testing.T) {
		p := &pattern.Pattern{
			Name:    testPatternPrefix + "create-empty-tags",
			Content: "Content with empty tags",
			Tags:    []string{},
		}

		err := repo.Create(ctx, p)
		require.NoError(t, err)

		retrieved, err := repo.Get(ctx, p.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.Tags)
		assert.Empty(t, retrieved.Tags)
	})

	t.Run("returns ErrNameExists for duplicate name", func(t *testing.T) {
		p1 := testIntegrationPattern("create-duplicate")
		require.NoError(t, repo.Create(ctx, p1))

		p2 := &pattern.Pattern{
			Name:    p1.Name, // Same name
			Content: "Different content",
			Tags:    []string{},
		}

		err := repo.Create(ctx, p2)
		assert.ErrorIs(t, err, pattern.ErrNameExists)
	})
}

func TestIntegration_Get(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	// Create a test pattern first
	testPattern := testIntegrationPattern("get-test")
	require.NoError(t, repo.Create(ctx, testPattern))

	t.Run("retrieves existing pattern by ID", func(t *testing.T) {
		retrieved, err := repo.Get(ctx, testPattern.ID)

		require.NoError(t, err)
		assert.Equal(t, testPattern.ID, retrieved.ID)
		assert.Equal(t, testPattern.Name, retrieved.Name)
		assert.Equal(t, *testPattern.Description, *retrieved.Description)
		assert.Equal(t, testPattern.Content, retrieved.Content)
		assert.Equal(t, testPattern.Tags, retrieved.Tags)
		assert.Equal(t, "pending", retrieved.EnrichmentStatus)
		assert.False(t, retrieved.CreatedAt.IsZero())
		assert.False(t, retrieved.UpdatedAt.IsZero())
	})

	t.Run("returns ErrNotFound for nonexistent ID", func(t *testing.T) {
		nonexistentID := uuid.New()
		retrieved, err := repo.Get(ctx, nonexistentID)

		assert.ErrorIs(t, err, pattern.ErrNotFound)
		assert.Nil(t, retrieved)
	})
}

func TestIntegration_Update(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	// Create a test pattern first
	testPattern := testIntegrationPattern("update-test")
	require.NoError(t, repo.Create(ctx, testPattern))
	originalCreatedAt := testPattern.CreatedAt

	t.Run("updates existing pattern", func(t *testing.T) {
		// Modify the pattern
		newDesc := "Updated description"
		testPattern.Description = &newDesc
		testPattern.Content = "Updated content for testing"
		testPattern.Tags = []string{"updated", "tags"}

		err := repo.Update(ctx, testPattern)
		require.NoError(t, err)
		assert.False(t, testPattern.UpdatedAt.IsZero())

		// Verify by reading back
		retrieved, err := repo.Get(ctx, testPattern.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated description", *retrieved.Description)
		assert.Equal(t, "Updated content for testing", retrieved.Content)
		assert.Equal(t, []string{"updated", "tags"}, retrieved.Tags)
		assert.Equal(t, originalCreatedAt.UTC().Truncate(time.Microsecond),
			retrieved.CreatedAt.UTC().Truncate(time.Microsecond),
			"CreatedAt should not change")
		assert.True(t, retrieved.UpdatedAt.After(retrieved.CreatedAt) ||
			retrieved.UpdatedAt.Equal(retrieved.CreatedAt),
			"UpdatedAt should be >= CreatedAt")
	})

	t.Run("returns ErrNotFound for nonexistent pattern", func(t *testing.T) {
		nonexistent := &pattern.Pattern{
			ID:      uuid.New(),
			Name:    testPatternPrefix + "nonexistent-update",
			Content: "Does not exist",
			Tags:    []string{},
		}

		err := repo.Update(ctx, nonexistent)
		assert.ErrorIs(t, err, pattern.ErrNotFound)
	})
}

func TestIntegration_Delete(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	t.Run("deletes existing pattern", func(t *testing.T) {
		// Create a test pattern
		testPattern := testIntegrationPattern("delete-test")
		require.NoError(t, repo.Create(ctx, testPattern))

		// Verify it exists
		_, err := repo.Get(ctx, testPattern.ID)
		require.NoError(t, err)

		// Delete it
		err = repo.Delete(ctx, testPattern.ID)
		require.NoError(t, err)

		// Verify it's gone
		_, err = repo.Get(ctx, testPattern.ID)
		assert.ErrorIs(t, err, pattern.ErrNotFound)
	})

	t.Run("returns ErrNotFound for nonexistent pattern", func(t *testing.T) {
		err := repo.Delete(ctx, uuid.New())
		assert.ErrorIs(t, err, pattern.ErrNotFound)
	})
}

func TestIntegration_List(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	// Create multiple test patterns with names that sort predictably
	patterns := []*pattern.Pattern{
		testIntegrationPattern("list-a"),
		testIntegrationPattern("list-b"),
		testIntegrationPattern("list-c"),
		testIntegrationPattern("list-d"),
		testIntegrationPattern("list-e"),
	}

	// Add specific tags for filtering tests
	patterns[0].Tags = []string{"go", "backend"}
	patterns[1].Tags = []string{"python", "backend"}
	patterns[2].Tags = []string{"go", "frontend"}

	for _, p := range patterns {
		require.NoError(t, repo.Create(ctx, p))
	}

	t.Run("lists all patterns without filters", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{}, repository.ListOptions{})

		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(result), 5)

		// Verify our test patterns are in the result
		names := make(map[string]bool)
		for _, p := range result {
			names[p.Name] = true
		}
		for _, expected := range patterns {
			assert.True(t, names[expected.Name], "expected pattern %s in result", expected.Name)
		}
	})

	t.Run("lists with limit", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{}, repository.ListOptions{Limit: 2})

		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("lists with limit and offset", func(t *testing.T) {
		// Get first page
		page1, err := repo.List(ctx, pattern.Filter{}, repository.ListOptions{Limit: 2, Offset: 0})
		require.NoError(t, err)
		assert.Len(t, page1, 2)

		// Get second page
		page2, err := repo.List(ctx, pattern.Filter{}, repository.ListOptions{Limit: 2, Offset: 2})
		require.NoError(t, err)
		assert.Len(t, page2, 2)

		// Pages should have different patterns
		for _, p1 := range page1 {
			for _, p2 := range page2 {
				assert.NotEqual(t, p1.ID, p2.ID, "pages should not overlap")
			}
		}
	})

	t.Run("lists with tag filter", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{
			Tags: []string{"go"},
		}, repository.ListOptions{})

		require.NoError(t, err)
		// Should include patterns with "go" tag
		for _, p := range result {
			if p.Name == patterns[0].Name || p.Name == patterns[2].Name {
				// These should be in the result
				assert.Contains(t, p.Tags, "go")
			}
		}
	})

	t.Run("returns ordered results", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{}, repository.ListOptions{})

		require.NoError(t, err)
		require.GreaterOrEqual(t, len(result), 2)

		// Verify alphabetical ordering by name
		for i := 1; i < len(result); i++ {
			assert.LessOrEqual(t, result[i-1].Name, result[i].Name,
				"results should be ordered by name")
		}
	})
}

func TestIntegration_Constraints(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	t.Run("accepts content over 10KB", func(t *testing.T) {
		// Migration 000005 dropped the old 10KB CHECK; the API handler caps
		// content at 100,000 bytes, so the database must store that much.
		largeContent := make([]byte, 100_000)
		for i := range largeContent {
			largeContent[i] = 'a' + byte(i%26)
		}

		p := &pattern.Pattern{
			Name:    testPatternPrefix + "large-content",
			Content: string(largeContent),
			Tags:    []string{},
		}

		err := repo.Create(ctx, p)
		require.NoError(t, err)

		retrieved, err := repo.Get(ctx, p.ID)
		require.NoError(t, err)
		assert.Equal(t, len(largeContent), len(retrieved.Content))
	})

	t.Run("accepts nil tags by converting to empty array", func(t *testing.T) {
		// Defensive check in Create() converts nil to empty slice
		p := &pattern.Pattern{
			Name:    testPatternPrefix + "nil-tags",
			Content: "Test content",
			Tags:    nil,
		}

		err := repo.Create(ctx, p)
		require.NoError(t, err)

		// Verify tags were saved as empty array
		retrieved, err := repo.Get(ctx, p.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.Tags)
		assert.Empty(t, retrieved.Tags)
	})

	t.Run("accepts empty tags array", func(t *testing.T) {
		p := &pattern.Pattern{
			Name:    testPatternPrefix + "empty-tags-constraint",
			Content: "Test content",
			Tags:    []string{},
		}

		err := repo.Create(ctx, p)
		assert.NoError(t, err)
	})
}

func TestIntegration_ContextCancellation(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)

	t.Run("respects cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := repo.Get(ctx, uuid.New())
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled),
			"expected context.Canceled error, got: %v", err)
	})

	t.Run("respects context timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Give time for the context to expire
		time.Sleep(1 * time.Millisecond)

		_, err := repo.Get(ctx, uuid.New())
		assert.Error(t, err)
	})
}

func TestIntegration_ConcurrentOperations(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	t.Run("handles concurrent creates", func(t *testing.T) {
		const numPatterns = 10
		var wg sync.WaitGroup
		errChan := make(chan error, numPatterns)

		for i := 0; i < numPatterns; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				p := &pattern.Pattern{
					Name:    fmt.Sprintf("%sconcurrent-%d", testPatternPrefix, idx),
					Content: fmt.Sprintf("Concurrent pattern %d", idx),
					Tags:    []string{},
				}
				errChan <- repo.Create(ctx, p)
			}(i)
		}

		wg.Wait()
		close(errChan)

		// Collect results
		var createErrors []error
		for err := range errChan {
			if err != nil {
				createErrors = append(createErrors, err)
			}
		}

		assert.Empty(t, createErrors, "all concurrent creates should succeed")

		// Verify all were created
		var created int
		err := pool.QueryRow(ctx,
			"SELECT count(*) FROM patterns WHERE name LIKE $1",
			testPatternPrefix+"concurrent-%").Scan(&created)
		require.NoError(t, err)
		assert.Equal(t, numPatterns, created)
	})

	t.Run("handles concurrent updates", func(t *testing.T) {
		// Create a pattern first
		p := testIntegrationPattern("concurrent-update")
		require.NoError(t, repo.Create(ctx, p))

		const numUpdates = 10
		var wg sync.WaitGroup
		errChan := make(chan error, numUpdates)

		for i := 0; i < numUpdates; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				updateP := &pattern.Pattern{
					ID:      p.ID,
					Name:    p.Name,
					Content: fmt.Sprintf("Updated content %d", idx),
					Tags:    []string{fmt.Sprintf("tag-%d", idx)},
				}
				errChan <- repo.Update(ctx, updateP)
			}(i)
		}

		wg.Wait()
		close(errChan)

		// Collect results - all should succeed
		var updateErrors []error
		for err := range errChan {
			if err != nil {
				updateErrors = append(updateErrors, err)
			}
		}

		assert.Empty(t, updateErrors, "all concurrent updates should succeed")
	})
}

func TestIntegration_ListWithEnrichmentStatusFilter(t *testing.T) {
	pool := setupTestDB(t)
	cleanupTestPatterns(t, pool)
	t.Cleanup(func() { cleanupTestPatterns(t, pool) })

	repo := pattern.NewRepository(pool)
	ctx := context.Background()

	// Create patterns with different enrichment statuses
	pendingPattern := testIntegrationPattern("status-filter-pending")
	require.NoError(t, repo.Create(ctx, pendingPattern))

	enrichedPattern := testIntegrationPattern("status-filter-enriched")
	require.NoError(t, repo.Create(ctx, enrichedPattern))
	// The enricher owns status transitions; set the column directly since it
	// is all List filters on here.
	setEnrichmentStatus(t, pool, enrichedPattern.ID, "enriched", nil)

	failedPattern := testIntegrationPattern("status-filter-failed")
	require.NoError(t, repo.Create(ctx, failedPattern))
	errMsg := "test error"
	setEnrichmentStatus(t, pool, failedPattern.ID, "failed", &errMsg)

	t.Run("filters by pending status", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{
			EnrichmentStatus: "pending",
		}, repository.ListOptions{})

		require.NoError(t, err)

		found := false
		for _, p := range result {
			assert.Equal(t, "pending", p.EnrichmentStatus)
			if p.Name == pendingPattern.Name {
				found = true
			}
		}
		assert.True(t, found, "pending pattern should be in results")
	})

	t.Run("filters by enriched status", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{
			EnrichmentStatus: "enriched",
		}, repository.ListOptions{})

		require.NoError(t, err)

		found := false
		for _, p := range result {
			assert.Equal(t, "enriched", p.EnrichmentStatus)
			if p.Name == enrichedPattern.Name {
				found = true
			}
		}
		assert.True(t, found, "enriched pattern should be in results")
	})

	t.Run("filters by failed status", func(t *testing.T) {
		result, err := repo.List(ctx, pattern.Filter{
			EnrichmentStatus: "failed",
		}, repository.ListOptions{})

		require.NoError(t, err)

		found := false
		for _, p := range result {
			assert.Equal(t, "failed", p.EnrichmentStatus)
			if p.Name == failedPattern.Name {
				found = true
			}
		}
		assert.True(t, found, "failed pattern should be in results")
	})
}

// setEnrichmentStatus writes a pattern's enrichment status directly, standing
// in for the enricher.
func setEnrichmentStatus(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, status string, errMsg *string) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		"UPDATE patterns SET enrichment_status = $2, enrichment_error = $3 WHERE id = $1",
		id, status, errMsg)
	require.NoError(t, err)
}
