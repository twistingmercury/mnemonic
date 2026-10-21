package pattern_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	graphrepo "github.com/twistingmercury/librarian/internal/mcp/repository/graph"
	patternrepo "github.com/twistingmercury/librarian/internal/mcp/repository/pattern"
	patternsvc "github.com/twistingmercury/librarian/internal/mcp/service/pattern"
)

// ---------- Mock: patternrepo.Repository ----------

type mockPatternRepo struct {
	mock.Mock
}

func (m *mockPatternRepo) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternRepo) GetName(ctx context.Context, id uuid.UUID) (string, error) {
	args := m.Called(ctx, id)
	return args.String(0), args.Error(1)
}

func (m *mockPatternRepo) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*patternrepo.Pattern, error) {
	args := m.Called(ctx, ids)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*patternrepo.Pattern), args.Error(1)
}

// ---------- Mock: graphrepo.Repository ----------

type mockGraphRepo struct {
	mock.Mock
}

func (m *mockGraphRepo) GetPatternConcepts(ctx context.Context, patternID uuid.UUID) ([]graphrepo.Concept, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]graphrepo.Concept), args.Error(1)
}

func (m *mockGraphRepo) FindRelatedPatterns(ctx context.Context, patternID uuid.UUID, limit int) ([]graphrepo.RelatedPattern, error) {
	args := m.Called(ctx, patternID, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]graphrepo.RelatedPattern), args.Error(1)
}

// ---------- Helpers ----------

var (
	testPatternID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testRelatedID = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

func newTestService(pr *mockPatternRepo, gr *mockGraphRepo) *patternsvc.Service {
	return patternsvc.New(pr, gr, zerolog.Nop())
}

func testPattern() *patternrepo.Pattern {
	return &patternrepo.Pattern{
		ID:               testPatternID,
		Name:             "go-error-handling",
		Content:          "Always handle errors explicitly.",
		Tags:             []string{"golang", "best-practices"},
		EnrichmentStatus: "pending",
	}
}

func enrichedPattern() *patternrepo.Pattern {
	p := testPattern()
	p.EnrichmentStatus = "enriched"
	return p
}

// ---------- GetPatternWithGraph ----------

func TestGetPatternWithGraph(t *testing.T) {
	t.Parallel()

	t.Run("enriched pattern with graph context", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		pr.On("Get", mock.Anything, testPatternID).Return(enrichedPattern(), nil)
		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 10).Return([]graphrepo.RelatedPattern{
			{
				ID:           testRelatedID,
				Name:         "go-concurrency",
				Similarity:   0.75,
				ConceptNames: []string{"goroutines", "channels"},
			},
		}, nil)
		gr.On("GetPatternConcepts", mock.Anything, testPatternID).Return([]graphrepo.Concept{
			{Name: "error-handling", Type: "practice"},
			{Name: "golang", Type: "technology"},
		}, nil)

		pattern, graphCtx, err := svc.GetPatternWithGraph(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, pattern)
		require.NotNil(t, graphCtx)

		assert.Len(t, graphCtx.RelatedPatterns, 1)
		assert.Equal(t, testRelatedID, graphCtx.RelatedPatterns[0].ID)
		assert.Equal(t, "go-concurrency", graphCtx.RelatedPatterns[0].Name)
		assert.Equal(t, "RELATED_TO", graphCtx.RelatedPatterns[0].Relationship)
		assert.InDelta(t, 0.75, graphCtx.RelatedPatterns[0].Similarity, 0.001)
		assert.Equal(t, []string{"goroutines", "channels"}, graphCtx.RelatedPatterns[0].SharedConcepts)

		assert.Len(t, graphCtx.Concepts, 2)
		assert.Equal(t, "error-handling", graphCtx.Concepts[0].Name)
		assert.Equal(t, "practice", graphCtx.Concepts[0].Type)

		pr.AssertExpectations(t)
		gr.AssertExpectations(t)
	})

	t.Run("pattern not enriched returns nil graph context", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil) // status = "pending"

		pattern, graphCtx, err := svc.GetPatternWithGraph(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, pattern)
		assert.Nil(t, graphCtx)

		gr.AssertNotCalled(t, "FindRelatedPatterns")
		gr.AssertNotCalled(t, "GetPatternConcepts")
	})

	t.Run("pattern not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		pattern, graphCtx, err := svc.GetPatternWithGraph(context.Background(), testPatternID)

		assert.ErrorIs(t, err, patternrepo.ErrNotFound)
		assert.Nil(t, pattern)
		assert.Nil(t, graphCtx)
	})

	t.Run("postgres failure is returned", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		dbErr := errors.New("connection refused")
		pr.On("Get", mock.Anything, testPatternID).Return(nil, dbErr)

		_, _, err := svc.GetPatternWithGraph(context.Background(), testPatternID)

		assert.ErrorIs(t, err, dbErr)
		assert.NotErrorIs(t, err, patternrepo.ErrNotFound)
	})

	for _, failing := range []string{"FindRelatedPatterns", "GetPatternConcepts"} {
		t.Run("neo4j "+failing+" failure returns nil graph context", func(t *testing.T) {
			t.Parallel()

			pr := new(mockPatternRepo)
			gr := new(mockGraphRepo)
			svc := newTestService(pr, gr)

			neoErr := errors.New("neo4j connection refused")
			pr.On("Get", mock.Anything, testPatternID).Return(enrichedPattern(), nil)
			if failing == "FindRelatedPatterns" {
				gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 10).Return(nil, neoErr)
				gr.On("GetPatternConcepts", mock.Anything, testPatternID).Return([]graphrepo.Concept{}, nil).Maybe()
			} else {
				gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 10).Return([]graphrepo.RelatedPattern{}, nil).Maybe()
				gr.On("GetPatternConcepts", mock.Anything, testPatternID).Return(nil, neoErr)
			}

			pattern, graphCtx, err := svc.GetPatternWithGraph(context.Background(), testPatternID)

			require.NoError(t, err, "neo4j failure should degrade gracefully")
			require.NotNil(t, pattern)
			assert.Nil(t, graphCtx)
		})
	}
}

// ---------- FindRelatedPatterns ----------

func TestFindRelatedPatterns(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		pr.On("GetName", mock.Anything, testPatternID).Return("go-error-handling", nil)
		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 5).Return([]graphrepo.RelatedPattern{
			{
				ID:           testRelatedID,
				Name:         "go-concurrency",
				Similarity:   0.8,
				ConceptNames: []string{"goroutines", "channels", "select"},
			},
		}, nil)

		name, results, err := svc.FindRelatedPatterns(context.Background(), testPatternID, 5)

		require.NoError(t, err)
		assert.Equal(t, "go-error-handling", name)
		require.Len(t, results, 1)
		assert.Equal(t, testRelatedID, results[0].ID)
		assert.Equal(t, "go-concurrency", results[0].Name)
		assert.Equal(t, "RELATED_TO", results[0].Relationship)
		assert.InDelta(t, 0.8, results[0].Similarity, 0.001)
		assert.Equal(t, []string{"goroutines", "channels", "select"}, results[0].SharedConcepts)

		pr.AssertExpectations(t)
		gr.AssertExpectations(t)
	})

	t.Run("pattern not found", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		pr.On("GetName", mock.Anything, testPatternID).Return("", patternrepo.ErrNotFound)
		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 5).Return([]graphrepo.RelatedPattern{}, nil).Maybe()

		name, results, err := svc.FindRelatedPatterns(context.Background(), testPatternID, 5)

		assert.Empty(t, name)
		assert.Nil(t, results)
		assert.ErrorIs(t, err, patternrepo.ErrNotFound)
	})

	t.Run("neo4j failure is returned", func(t *testing.T) {
		t.Parallel()

		pr := new(mockPatternRepo)
		gr := new(mockGraphRepo)
		svc := newTestService(pr, gr)

		neoErr := errors.New("neo4j connection refused")
		pr.On("GetName", mock.Anything, testPatternID).Return("go-error-handling", nil).Maybe()
		gr.On("FindRelatedPatterns", mock.Anything, testPatternID, 5).Return(nil, neoErr)

		_, results, err := svc.FindRelatedPatterns(context.Background(), testPatternID, 5)

		assert.Nil(t, results)
		assert.ErrorIs(t, err, neoErr)
	})
}
