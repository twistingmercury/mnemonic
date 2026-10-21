package graph_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/twistingmercury/librarian/internal/api/repository/graph"
)

// Nil IDs are rejected before the driver is touched, so a nil driver is fine.
func TestRepository_RejectsNilPatternID(t *testing.T) {
	repo := graph.NewRepository(nil, "neo4j", nil)
	ctx := context.Background()

	assert.ErrorContains(t, repo.DeletePattern(ctx, uuid.Nil), "patternID must not be nil UUID")

	_, err := repo.GetPatternGraph(ctx, uuid.Nil, 10)
	assert.ErrorContains(t, err, "patternID must not be nil UUID")
}
