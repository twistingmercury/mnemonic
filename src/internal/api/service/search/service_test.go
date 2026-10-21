package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/api/repository/chunk"
	"github.com/twistingmercury/mnemonic/internal/api/service"
	openaisvc "github.com/twistingmercury/mnemonic/internal/api/service/openai"
	"github.com/twistingmercury/mnemonic/internal/api/service/search"
)

// --- Mock: openaisvc.EmbeddingService ---

type mockEmbeddingService struct {
	mock.Mock
}

func (m *mockEmbeddingService) Embed(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]float32), args.Error(1)
}

// --- Mock: chunkrepo.Repository ---

// mockChunkRepo implements only FindSimilar, the one method search uses; any
// other call panics on the nil embedded interface.
type mockChunkRepo struct {
	chunkrepo.Repository
	mock.Mock
}

func (m *mockChunkRepo) FindSimilar(ctx context.Context, embedding []float32, opts chunkrepo.SimilarityOptions) ([]*chunkrepo.Match, error) {
	args := m.Called(ctx, embedding, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Match), args.Error(1)
}

// --- Helpers ---

var testEmbedding = []float32{0.1, 0.2, 0.3}

func newTestService(embSvc *mockEmbeddingService, chunkRepo *mockChunkRepo) search.Service {
	logger := zerolog.Nop()
	return search.New(embSvc, chunkRepo, nil, logger)
}

func testChunkMatch(patternID uuid.UUID, patternName string, similarity float64) *chunkrepo.Match {
	return &chunkrepo.Match{
		PatternID:    patternID,
		PatternName:  patternName,
		EntityType:   "go-pattern",
		Language:     "go",
		Domain:       "backend",
		Tags:         []string{"go", "testing"},
		SectionTitle: "Overview",
		ChunkIndex:   0,
		Content:      "test content for " + patternName,
		Similarity:   similarity,
	}
}

// ---------- SearchPatterns ----------

func TestSearchPatterns_HappyPath(t *testing.T) {
	t.Parallel()

	embSvc := new(mockEmbeddingService)
	chunkRepo := new(mockChunkRepo)
	svc := newTestService(embSvc, chunkRepo)

	id1 := uuid.New()
	id2 := uuid.New()

	embSvc.On("Embed", mock.Anything, "error handling in Go").Return(testEmbedding, nil)
	chunkRepo.On("FindSimilar", mock.Anything, testEmbedding, chunkrepo.SimilarityOptions{
		MinSimilarity: 0.7,
		MaxResults:    10,
	}).Return([]*chunkrepo.Match{
		testChunkMatch(id1, "go-error-handling", 0.92),
		testChunkMatch(id2, "go-error-wrapping", 0.85),
	}, nil)

	result, err := svc.SearchPatterns(context.Background(), search.SearchOptions{
		Query:     "error handling in Go",
		Limit:     10,
		Threshold: 0.7,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "error handling in Go", result.Query)
	assert.Len(t, result.Matches, 2)
	assert.Equal(t, 2, result.TotalCandidates)
	assert.Greater(t, result.SearchDurationMs, int64(-1))
	assert.Equal(t, id1, result.Matches[0].PatternID)
	assert.Equal(t, "go-error-handling", result.Matches[0].PatternName)
	assert.InDelta(t, 0.92, result.Matches[0].Similarity, 0.001)

	embSvc.AssertExpectations(t)
	chunkRepo.AssertExpectations(t)
}

func TestSearchPatterns_EmbeddingFailure(t *testing.T) {
	t.Parallel()

	embSvc := new(mockEmbeddingService)
	chunkRepo := new(mockChunkRepo)
	svc := newTestService(embSvc, chunkRepo)

	embSvc.On("Embed", mock.Anything, "some query").Return(nil, openaisvc.ErrEmbeddingFailed)

	result, err := svc.SearchPatterns(context.Background(), search.SearchOptions{
		Query:     "some query",
		Limit:     10,
		Threshold: 0.7,
	})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.True(t, errors.Is(err, service.ErrServiceUnavailable), "expected service.ErrServiceUnavailable, got: %v", err)

	chunkRepo.AssertNotCalled(t, "FindSimilar")
}

func TestSearchPatterns_NoMatchingPatterns(t *testing.T) {
	t.Parallel()

	embSvc := new(mockEmbeddingService)
	chunkRepo := new(mockChunkRepo)
	svc := newTestService(embSvc, chunkRepo)

	embSvc.On("Embed", mock.Anything, "obscure topic").Return(testEmbedding, nil)
	chunkRepo.On("FindSimilar", mock.Anything, testEmbedding, chunkrepo.SimilarityOptions{
		MinSimilarity: 0.9,
		MaxResults:    5,
	}).Return([]*chunkrepo.Match{}, nil)

	result, err := svc.SearchPatterns(context.Background(), search.SearchOptions{
		Query:     "obscure topic",
		Limit:     5,
		Threshold: 0.9,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Matches)
	assert.Equal(t, "obscure topic", result.Query)
	assert.Equal(t, 0, result.TotalCandidates)
}

func TestSearchPatterns_WithTags(t *testing.T) {
	t.Parallel()

	embSvc := new(mockEmbeddingService)
	chunkRepo := new(mockChunkRepo)
	svc := newTestService(embSvc, chunkRepo)

	id1 := uuid.New()

	embSvc.On("Embed", mock.Anything, "go patterns").Return(testEmbedding, nil)
	chunkRepo.On("FindSimilar", mock.Anything, testEmbedding, chunkrepo.SimilarityOptions{
		MinSimilarity: 0.7,
		MaxResults:    10,
		Tags:          []string{"go", "best-practices"},
	}).Return([]*chunkrepo.Match{
		testChunkMatch(id1, "go-best-practices", 0.91),
	}, nil)

	result, err := svc.SearchPatterns(context.Background(), search.SearchOptions{
		Query:     "go patterns",
		Limit:     10,
		Threshold: 0.7,
		Tags:      []string{"go", "best-practices"},
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Matches, 1)

	embSvc.AssertExpectations(t)
	chunkRepo.AssertExpectations(t)
}

func TestSearchPatterns_FindSimilarError(t *testing.T) {
	t.Parallel()

	embSvc := new(mockEmbeddingService)
	chunkRepo := new(mockChunkRepo)
	svc := newTestService(embSvc, chunkRepo)

	embSvc.On("Embed", mock.Anything, "some query").Return(testEmbedding, nil)
	chunkRepo.On("FindSimilar", mock.Anything, testEmbedding, mock.Anything).
		Return(nil, errors.New("database connection lost"))

	result, err := svc.SearchPatterns(context.Background(), search.SearchOptions{
		Query:     "some query",
		Limit:     10,
		Threshold: 0.7,
	})

	assert.Nil(t, result)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "find similar chunks")
}
