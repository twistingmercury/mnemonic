package pattern_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/mnemonic/internal/api/repository"
	chunkrepo "github.com/twistingmercury/mnemonic/internal/api/repository/chunk"
	enrichmentrepo "github.com/twistingmercury/mnemonic/internal/api/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/mnemonic/internal/api/repository/graph"
	patternrepo "github.com/twistingmercury/mnemonic/internal/api/repository/pattern"
	"github.com/twistingmercury/mnemonic/internal/api/service"
	patternsvc "github.com/twistingmercury/mnemonic/internal/api/service/pattern"
)

// ---------- Mock: patternrepo.Repository ----------

// mockPatternRepo distinguishes writes issued inside a transaction from writes
// issued against the pool. WithTx hands back a separate mock, so a service that
// mutates through the outer repository finds no expectation and fails the test.
type mockPatternRepo struct {
	mock.Mock
	txRepo *mockPatternRepo
}

// tx returns the repository WithTx will hand back, so a test can set
// expectations on it before the service runs.
func (m *mockPatternRepo) tx() *mockPatternRepo {
	if m.txRepo == nil {
		m.txRepo = &mockPatternRepo{}
	}
	return m.txRepo
}

func (m *mockPatternRepo) Create(ctx context.Context, pattern *patternrepo.Pattern) error {
	args := m.Called(ctx, pattern)
	return args.Error(0)
}

func (m *mockPatternRepo) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternRepo) Update(ctx context.Context, pattern *patternrepo.Pattern) error {
	args := m.Called(ctx, pattern)
	return args.Error(0)
}

func (m *mockPatternRepo) Delete(ctx context.Context, id uuid.UUID) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockPatternRepo) List(ctx context.Context, filter patternrepo.Filter, opts repository.ListOptions) ([]*patternrepo.Pattern, error) {
	args := m.Called(ctx, filter, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*patternrepo.Pattern), args.Error(1)
}

func (m *mockPatternRepo) WithTx(_ repository.DBTX) patternrepo.Repository {
	return m.tx()
}

// ---------- Mock: enrichmentrepo.Repository ----------

type mockEnrichmentRepo struct {
	mock.Mock
	txRepo *mockEnrichmentRepo
}

func (m *mockEnrichmentRepo) CreatePendingForChunks(ctx context.Context, chunkIDs []uuid.UUID) ([]uuid.UUID, error) {
	args := m.Called(ctx, chunkIDs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]uuid.UUID), args.Error(1)
}

func (m *mockEnrichmentRepo) WithTx(_ repository.DBTX) enrichmentrepo.Repository {
	return m.tx()
}

// tx returns the repository WithTx will hand back. See mockPatternRepo.tx.
func (m *mockEnrichmentRepo) tx() *mockEnrichmentRepo {
	if m.txRepo == nil {
		m.txRepo = &mockEnrichmentRepo{}
	}
	return m.txRepo
}

// ---------- Mock: graphrepo.Repository ----------

type mockGraphRepo struct {
	mock.Mock
}

func (m *mockGraphRepo) DeletePattern(ctx context.Context, patternID uuid.UUID) error {
	args := m.Called(ctx, patternID)
	return args.Error(0)
}

func (m *mockGraphRepo) GetPatternGraph(ctx context.Context, patternID uuid.UUID, relatedLimit int) (*graphrepo.PatternGraph, error) {
	args := m.Called(ctx, patternID, relatedLimit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*graphrepo.PatternGraph), args.Error(1)
}

// ---------- Mock: repository.TxBeginner ----------

type mockTxBeginner struct {
	mock.Mock
}

func (m *mockTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Tx), args.Error(1)
}

// ---------- Mock: pgx.Tx ----------

// mockPgxTx records the transaction lifecycle (Commit and Rollback) inside the
// service layer. The mock repositories intercept every data-access call, so
// the other pgx.Tx methods are left to the nil embedded interface and panic
// if anything reaches them.
type mockPgxTx struct {
	pgx.Tx
	mock.Mock
}

func (m *mockPgxTx) Commit(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockPgxTx) Rollback(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// ---------- Mock: chunkrepo.Repository ----------

type mockChunkRepo struct {
	mock.Mock
	txRepo *mockChunkRepo
}

func (m *mockChunkRepo) CreateBatch(ctx context.Context, chunks []*chunkrepo.Chunk) error {
	args := m.Called(ctx, chunks)
	// Assign IDs to chunks so that enrichment job creation can use them.
	if args.Error(0) == nil {
		for _, c := range chunks {
			if c.ID == (uuid.UUID{}) {
				c.ID = uuid.New()
			}
		}
	}
	return args.Error(0)
}

func (m *mockChunkRepo) ListSummariesByPatternID(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error) {
	args := m.Called(ctx, patternID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Chunk), args.Error(1)
}

func (m *mockChunkRepo) DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error {
	args := m.Called(ctx, patternID)
	return args.Error(0)
}

func (m *mockChunkRepo) FindSimilar(ctx context.Context, embedding []float32, opts chunkrepo.SimilarityOptions) ([]*chunkrepo.Match, error) {
	args := m.Called(ctx, embedding, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*chunkrepo.Match), args.Error(1)
}

func (m *mockChunkRepo) WithTx(_ repository.DBTX) chunkrepo.Repository {
	return m.tx()
}

// tx returns the repository WithTx will hand back. See mockPatternRepo.tx.
func (m *mockChunkRepo) tx() *mockChunkRepo {
	if m.txRepo == nil {
		m.txRepo = &mockChunkRepo{}
	}
	return m.txRepo
}

// ---------- Mock: queue.Publisher ----------

type mockPublisher struct {
	// attemptedIDs records every call, publishedIDs only the successful ones.
	// Without the first, a test cannot distinguish a failed publish from a
	// publish that was never attempted.
	attemptedIDs []uuid.UUID
	publishedIDs []uuid.UUID
	publishErr   error
}

func (m *mockPublisher) Publish(_ context.Context, jobID uuid.UUID) error {
	m.attemptedIDs = append(m.attemptedIDs, jobID)
	if m.publishErr != nil {
		return m.publishErr
	}
	m.publishedIDs = append(m.publishedIDs, jobID)
	return nil
}

func (m *mockPublisher) Close() error { return nil }

// ---------- Helpers ----------

var (
	testPatternID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testRelatedID = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

// fixture is a pattern service wired to fresh mocks.
type fixture struct {
	pr  *mockPatternRepo
	er  *mockEnrichmentRepo
	gr  *mockGraphRepo
	tb  *mockTxBeginner
	cr  *mockChunkRepo
	pub *mockPublisher
	svc patternsvc.Service
}

func newFx() *fixture {
	f := &fixture{
		pr:  new(mockPatternRepo),
		er:  new(mockEnrichmentRepo),
		gr:  new(mockGraphRepo),
		tb:  new(mockTxBeginner),
		cr:  new(mockChunkRepo),
		pub: &mockPublisher{},
	}
	f.svc = patternsvc.New(f.pr, f.er, f.gr, f.tb, f.cr, f.pub, zerolog.Nop())
	return f
}

func testCreateInput() patternsvc.Input {
	desc := "A test pattern"
	return patternsvc.Input{
		Name:        "go-error-handling",
		Description: &desc,
		Content:     "Always handle errors explicitly.",
		Tags:        []string{"golang", "best-practices"},
	}
}

func testUpdateInput() patternsvc.Input {
	desc := "Updated description"
	return patternsvc.Input{
		Name:        "go-error-handling-v2",
		Description: &desc,
		Content:     "Updated content.",
		Tags:        []string{"golang", "errors"},
	}
}

func testPattern() *patternrepo.Pattern {
	desc := "A test pattern"
	return &patternrepo.Pattern{
		ID:               testPatternID,
		Name:             "go-error-handling",
		Description:      &desc,
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

// expectCommittedTx wires a transaction that begins and commits cleanly. The
// deferred Rollback still runs and is a no-op once committed, so it is
// permitted but not required.
func expectCommittedTx(tb *mockTxBeginner) *mockPgxTx {
	tx := new(mockPgxTx)
	tb.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("Commit", mock.Anything).Return(nil)
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed).Maybe()
	return tx
}

// expectRolledBackTx wires a transaction that begins and must never commit.
// Commit carries no expectation, so a service that commits anyway fails here.
func expectRolledBackTx(tb *mockTxBeginner) *mockPgxTx {
	tx := new(mockPgxTx)
	tb.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("Rollback", mock.Anything).Return(nil)
	return tx
}

// ---------- Create ----------

func TestCreate(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		expectCommittedTx(f.tb)
		// The write must go through the transaction, not the pool.
		f.pr.tx().On("Create", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
			return p.Name == "go-error-handling" && p.Content == "Always handle errors explicitly."
		})).Run(func(args mock.Arguments) {
			p := args.Get(1).(*patternrepo.Pattern)
			p.ID = testPatternID
			p.EnrichmentStatus = "pending"
		}).Return(nil)
		// Undecorated content becomes one whole-content chunk with one job.
		f.cr.tx().On("CreateBatch", mock.Anything, mock.MatchedBy(func(chunks []*chunkrepo.Chunk) bool {
			return len(chunks) == 1 && chunks[0].Content == "Always handle errors explicitly."
		})).Return(nil)
		f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
			return len(ids) == 1
		})).Return([]uuid.UUID{uuid.New()}, nil).Once()

		result, err := f.svc.Create(context.Background(), testCreateInput())

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "go-error-handling", result.Name)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "pending", result.EnrichmentStatus)

		f.pr.tx().AssertExpectations(t)
		f.tb.AssertExpectations(t)
		f.gr.AssertExpectations(t)
	})

	t.Run("pattern name conflict returns service.ErrConflict", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		expectRolledBackTx(f.tb)
		f.pr.tx().On("Create", mock.Anything, mock.Anything).Return(patternrepo.ErrNameExists)

		result, err := f.svc.Create(context.Background(), testCreateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrConflict), "expected service.ErrConflict, got: %v", err)

		f.er.AssertNotCalled(t, "CreatePendingForChunks")
		f.er.tx().AssertNotCalled(t, "CreatePendingForChunks")
		f.tb.AssertExpectations(t)
	})

	t.Run("begin failure never touches a repository", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.tb.On("Begin", mock.Anything).Return(nil, errors.New("pool exhausted"))

		result, err := f.svc.Create(context.Background(), testCreateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool exhausted")
		f.pr.AssertNotCalled(t, "Create")
		f.pr.tx().AssertNotCalled(t, "Create")
		f.tb.AssertExpectations(t)
	})
}

// ---------- Get ----------

func TestGet(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)

		result, err := f.svc.Get(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "go-error-handling", result.Name)

		f.pr.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		result, err := f.svc.Get(context.Background(), testPatternID)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)
	})
}

// ---------- GetWithGraph ----------

func TestGetWithGraph(t *testing.T) {
	t.Parallel()

	t.Run("enriched pattern with graph context", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Get", mock.Anything, testPatternID).Return(enrichedPattern(), nil)

		f.gr.On("GetPatternGraph", mock.Anything, testPatternID, 10).Return(&graphrepo.PatternGraph{
			RelatedPatterns: []graphrepo.RelatedPattern{
				{
					ID:             testRelatedID,
					Name:           "go-concurrency",
					SharedConcepts: 2,
					Similarity:     0.75,
					ConceptNames:   []string{"goroutines", "channels"},
				},
			},
			Concepts: []graphrepo.Concept{
				{Name: "error-handling"},
				{Name: "golang"},
			},
		}, nil)

		pattern, graphCtx, err := f.svc.GetWithGraph(context.Background(), testPatternID)

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

		f.pr.AssertExpectations(t)
		f.gr.AssertExpectations(t)
	})

	t.Run("pattern not enriched returns nil graph context", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil) // status = "pending"

		pattern, graphCtx, err := f.svc.GetWithGraph(context.Background(), testPatternID)

		require.NoError(t, err)
		require.NotNil(t, pattern)
		assert.Nil(t, graphCtx)

		f.gr.AssertNotCalled(t, "GetPatternGraph")
	})

	t.Run("neo4j failure returns nil graph context", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Get", mock.Anything, testPatternID).Return(enrichedPattern(), nil)
		f.gr.On("GetPatternGraph", mock.Anything, testPatternID, 10).
			Return(nil, errors.New("neo4j connection refused"))

		pattern, graphCtx, err := f.svc.GetWithGraph(context.Background(), testPatternID)

		require.NoError(t, err, "neo4j failure should degrade gracefully")
		require.NotNil(t, pattern)
		assert.Nil(t, graphCtx)
	})
}

// ---------- Update ----------

func TestUpdate(t *testing.T) {
	t.Parallel()

	t.Run("happy path replaces chunks and jobs in one committed transaction, then publishes", func(t *testing.T) {
		t.Parallel()

		f := newFx()
		tx := expectCommittedTx(f.tb)
		jobIDs := []uuid.UUID{uuid.New(), uuid.New()}

		f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		f.pr.tx().On("Update", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
			return p.ID == testPatternID && p.Name == "go-error-handling-v2" && p.Content == decoratedContent
		})).Return(nil)
		f.cr.tx().On("DeleteByPatternID", mock.Anything, testPatternID).Return(nil)
		// decoratedContent has two [//]: pattern sections, so two chunks and two jobs.
		f.cr.tx().On("CreateBatch", mock.Anything, mock.MatchedBy(func(chunks []*chunkrepo.Chunk) bool {
			return len(chunks) == 2 && chunks[0].SectionTitle == "Section One" && chunks[1].SectionTitle == "Section Two"
		})).Return(nil)
		f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.MatchedBy(func(ids []uuid.UUID) bool {
			return len(ids) == 2
		})).Return(jobIDs, nil).Once()

		result, err := f.svc.Update(context.Background(), testPatternID, patternsvc.Input{
			Name:    "go-error-handling-v2",
			Content: decoratedContent,
		})

		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPatternID, result.ID)
		assert.Equal(t, "go-error-handling-v2", result.Name)

		// Every write went through the transaction, which committed. A service
		// that opened a transaction and then mutated via the pool repositories
		// would satisfy the lifecycle assertion but fail the pool ones.
		tx.AssertCalled(t, "Commit", mock.Anything)
		f.pr.tx().AssertExpectations(t)
		f.cr.tx().AssertExpectations(t)
		f.er.tx().AssertExpectations(t)
		f.pr.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
		f.cr.AssertNotCalled(t, "DeleteByPatternID", mock.Anything, mock.Anything)
		f.cr.AssertNotCalled(t, "CreateBatch", mock.Anything, mock.Anything)
		f.er.AssertNotCalled(t, "CreatePendingForChunks", mock.Anything, mock.Anything)

		// The new jobs are published only after the commit, in chunk order.
		assert.Equal(t, jobIDs, f.pub.publishedIDs)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)

		result, err := f.svc.Update(context.Background(), testPatternID, testUpdateInput())

		assert.Nil(t, result)
		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)

		f.pr.AssertNotCalled(t, "Update")
	})

	t.Run("chunk-aware path: delete stale chunks error rolls back transaction and propagates error", func(t *testing.T) {
		t.Parallel()

		f := newFx()
		tx := new(mockPgxTx)

		input := patternsvc.Input{
			Name:    "go-error-handling-v2",
			Content: "## Section\nContent.",
		}

		// Transaction lifecycle: Begin succeeds, Rollback is called by defer.
		f.tb.On("Begin", mock.Anything).Return(tx, nil)
		tx.On("Rollback", mock.Anything).Return(nil)

		f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		f.pr.tx().On("Update", mock.Anything, mock.Anything).Return(nil)
		f.cr.tx().On("DeleteByPatternID", mock.Anything, testPatternID).
			Return(errors.New("delete failed"))

		result, err := f.svc.Update(context.Background(), testPatternID, input)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete stale chunks")

		f.tb.AssertExpectations(t)
		tx.AssertExpectations(t)
		tx.AssertNotCalled(t, "Commit")
		f.cr.AssertNotCalled(t, "CreateBatch")
		f.er.AssertNotCalled(t, "CreatePendingForChunks")
	})

	t.Run("transaction rollback path: CreateBatch fails, Commit NOT called", func(t *testing.T) {
		t.Parallel()

		f := newFx()
		tx := new(mockPgxTx)

		input := patternsvc.Input{
			Name:    "go-error-handling-v2",
			Content: "[//]: pattern\n## Section One\nNew content.",
		}

		f.tb.On("Begin", mock.Anything).Return(tx, nil)
		tx.On("Rollback", mock.Anything).Return(nil)

		f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		f.pr.tx().On("Update", mock.Anything, mock.Anything).Return(nil)
		f.cr.tx().On("DeleteByPatternID", mock.Anything, testPatternID).Return(nil)
		f.cr.tx().On("CreateBatch", mock.Anything, mock.Anything).
			Return(errors.New("db error"))

		result, err := f.svc.Update(context.Background(), testPatternID, input)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chunk batch")

		tx.AssertNotCalled(t, "Commit")
		tx.AssertCalled(t, "Rollback", mock.Anything)

		f.tb.AssertExpectations(t)
		tx.AssertExpectations(t)
	})

	t.Run("transaction rollback path: Update fails, later steps NOT called", func(t *testing.T) {
		t.Parallel()

		f := newFx()
		tx := new(mockPgxTx)

		input := patternsvc.Input{
			Name:    "go-error-handling-v2",
			Content: "[//]: pattern\n## Section One\nNew content.",
		}

		f.tb.On("Begin", mock.Anything).Return(tx, nil)
		tx.On("Rollback", mock.Anything).Return(nil)

		f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
		f.pr.tx().On("Update", mock.Anything, mock.Anything).Return(errors.New("update failed"))

		result, err := f.svc.Update(context.Background(), testPatternID, input)

		assert.Nil(t, result)
		require.Error(t, err)

		tx.AssertNotCalled(t, "Commit")
		tx.AssertCalled(t, "Rollback", mock.Anything)
		f.cr.AssertNotCalled(t, "DeleteByPatternID")
		f.cr.AssertNotCalled(t, "CreateBatch")

		f.tb.AssertExpectations(t)
		tx.AssertExpectations(t)
	})
}

// ---------- Chunk enrichment job failure summary warnings ----------

// decoratedContent produces two chunks, and therefore two enrichment jobs.
const decoratedContent = "[//]: pattern\n## Section One\nContent one.\n\n[//]: pattern\n## Section Two\nContent two."

// TestCreate_JobFailureRollsBackEverything replaces an earlier test that
// asserted job-insert failures were swallowed and summarised in a warning.
// Required job rows now live in the same transaction as the pattern and its
// chunks, so a failure there must surface and persist nothing.
func TestCreate_JobFailureRollsBackEverything(t *testing.T) {
	t.Parallel()

	f := newFx()

	expectRolledBackTx(f.tb)
	f.pr.tx().On("Create", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*patternrepo.Pattern).ID = testPatternID
	}).Return(nil)
	f.cr.tx().On("CreateBatch", mock.Anything, mock.Anything).Return(nil)
	f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.Anything).Return(nil, errors.New("jobs table unavailable"))

	result, err := f.svc.Create(context.Background(), patternsvc.Input{
		Name:    "go-error-handling",
		Content: decoratedContent,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "jobs table unavailable")
	// Nothing may be published for a transaction that never committed.
	assert.Empty(t, f.pub.publishedIDs)
	f.tb.AssertExpectations(t)
	f.pr.tx().AssertExpectations(t)
}

// TestUpdate_JobFailureRollsBackEverything is the Update counterpart. It
// matters more than the Create case: Update has already deleted the old chunks
// inside this transaction, cascading their jobs away, so committing without
// the new jobs would leave the pattern with no route back to enriched.
func TestUpdate_JobFailureRollsBackEverything(t *testing.T) {
	t.Parallel()

	f := newFx()

	expectRolledBackTx(f.tb)
	f.pr.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	f.pr.tx().On("Update", mock.Anything, mock.Anything).Return(nil)
	f.cr.tx().On("DeleteByPatternID", mock.Anything, testPatternID).Return(nil)
	f.cr.tx().On("CreateBatch", mock.Anything, mock.Anything).Return(nil)
	f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.Anything).Return(nil, errors.New("jobs table unavailable"))

	result, err := f.svc.Update(context.Background(), testPatternID, patternsvc.Input{
		Name:    "go-error-handling-v2",
		Content: decoratedContent,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Empty(t, f.pub.publishedIDs)
	f.tb.AssertExpectations(t)
	f.cr.tx().AssertExpectations(t)
}

// TestCreate_CommitFailurePublishesNothing pins the ordering that makes
// best-effort publication safe: job IDs are collected inside the transaction
// but must not reach the queue unless the commit succeeds, or the enricher
// would claim rows that were rolled back.
func TestCreate_CommitFailurePublishesNothing(t *testing.T) {
	t.Parallel()

	f := newFx()

	tx := new(mockPgxTx)
	f.tb.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("Commit", mock.Anything).Return(errors.New("connection lost"))
	tx.On("Rollback", mock.Anything).Return(nil)

	f.pr.tx().On("Create", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*patternrepo.Pattern).ID = testPatternID
	}).Return(nil)
	f.cr.tx().On("CreateBatch", mock.Anything, mock.Anything).Return(nil)
	f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.Anything).Return([]uuid.UUID{uuid.New(), uuid.New()}, nil)

	result, err := f.svc.Create(context.Background(), patternsvc.Input{
		Name:    "go-error-handling",
		Content: decoratedContent,
	})

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "connection lost")
	assert.Empty(t, f.pub.publishedIDs, "a rolled-back transaction must publish nothing")
	tx.AssertExpectations(t)
}

func TestDelete(t *testing.T) {
	t.Parallel()

	t.Run("happy path deletes from neo4j once, with no graph-wide sweep", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Delete", mock.Anything, testPatternID).Return(nil)
		f.gr.On("DeletePattern", mock.Anything, testPatternID).Return(nil).Once()

		err := f.svc.Delete(context.Background(), testPatternID)

		require.NoError(t, err)

		f.pr.AssertExpectations(t)
		f.gr.AssertExpectations(t)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.pr.On("Delete", mock.Anything, testPatternID).Return(patternrepo.ErrNotFound)

		err := f.svc.Delete(context.Background(), testPatternID)

		require.Error(t, err)
		assert.True(t, errors.Is(err, service.ErrNotFound), "expected service.ErrNotFound, got: %v", err)

		f.gr.AssertNotCalled(t, "DeletePattern")
	})
}

// ---------- TestCreate_ChunksContent ----------

// TestCreate_ChunksContent follows one create through the whole chain and ties
// the identities together: each chunk gets its own job, and exactly those job
// IDs reach the queue, in order, only after the commit. Checking counts alone
// would not catch a service that published the wrong job for a chunk.
func TestCreate_ChunksContent(t *testing.T) {
	t.Parallel()

	f := newFx()

	chunkIDs := []uuid.UUID{
		uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		uuid.MustParse("33333333-3333-3333-3333-333333333333"),
	}
	jobIDs := []uuid.UUID{
		uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002"),
	}
	// Records which chunk each job was created for, so the published IDs can be
	// traced back to their chunks rather than merely counted.
	jobForChunk := map[uuid.UUID]uuid.UUID{}

	desc := "A chunked pattern"
	input := patternsvc.Input{
		Name:        "chunked-pattern",
		Description: &desc,
		Content:     decoratedContent,
		Tags:        []string{"test"},
		EntityType:  "go-pattern",
		Language:    "go",
		Domain:      "backend",
	}

	expectCommittedTx(f.tb)

	f.pr.tx().On("Create", mock.Anything, mock.MatchedBy(func(p *patternrepo.Pattern) bool {
		return p.Name == "chunked-pattern" && p.EntityType == "go-pattern" && p.Language == "go"
	})).Run(func(args mock.Arguments) {
		p := args.Get(1).(*patternrepo.Pattern)
		p.ID = testPatternID
		p.EnrichmentStatus = "pending"
	}).Return(nil)

	var created []*chunkrepo.Chunk
	f.cr.tx().On("CreateBatch", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		created = args.Get(1).([]*chunkrepo.Chunk)
		// Stand in for the IDs CreateBatch assigns.
		for i, c := range created {
			c.ID = chunkIDs[i]
		}
	}).Return(nil)

	f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		for i, chunkID := range args.Get(1).([]uuid.UUID) {
			jobForChunk[chunkID] = jobIDs[i]
		}
	}).Return(jobIDs, nil).Once()

	result, err := f.svc.Create(context.Background(), input)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, testPatternID, result.ID)

	require.Len(t, created, 2)
	assert.Equal(t, "Section One", created[0].SectionTitle)
	assert.Equal(t, "Section Two", created[1].SectionTitle)
	for i, c := range created {
		assert.Equal(t, i, c.ChunkIndex)
		assert.Equal(t, testPatternID, c.PatternID)
		assert.NotEmpty(t, c.Content)
	}

	// One job per chunk, each naming its own chunk.
	assert.Equal(t, map[uuid.UUID]uuid.UUID{
		chunkIDs[0]: jobIDs[0],
		chunkIDs[1]: jobIDs[1],
	}, jobForChunk)

	// Exactly those jobs are published, in chunk order, after the commit.
	assert.Equal(t, jobIDs, f.pub.publishedIDs)

	f.pr.tx().AssertExpectations(t)
	f.cr.tx().AssertExpectations(t)
	f.er.tx().AssertExpectations(t)
	f.tb.AssertExpectations(t)
}

func TestListChunks(t *testing.T) {
	t.Parallel()

	t.Run("happy path returns chunk summaries without re-reading the pattern", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		expectedChunks := []*chunkrepo.Chunk{
			{ChunkIndex: 0, SectionTitle: "Overview", EnrichmentStatus: "pending"},
			{ChunkIndex: 1, SectionTitle: "Philosophy", EnrichmentStatus: "enriched"},
		}
		f.cr.On("ListSummariesByPatternID", mock.Anything, testPatternID).Return(expectedChunks, nil)

		result, err := f.svc.ListChunks(context.Background(), testPatternID)

		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, 0, result[0].ChunkIndex)
		assert.Equal(t, "Overview", result[0].SectionTitle)
		assert.Equal(t, "pending", result[0].EnrichmentStatus)
		assert.Equal(t, 1, result[1].ChunkIndex)
		assert.Equal(t, "Philosophy", result[1].SectionTitle)

		f.pr.AssertNotCalled(t, "Get")
		f.cr.AssertExpectations(t)
	})

	t.Run("returns empty slice when pattern has no chunks", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.cr.On("ListSummariesByPatternID", mock.Anything, testPatternID).Return([]*chunkrepo.Chunk{}, nil)

		result, err := f.svc.ListChunks(context.Background(), testPatternID)

		require.NoError(t, err)
		assert.Empty(t, result)

		f.pr.AssertNotCalled(t, "Get")
		f.cr.AssertExpectations(t)
	})

	t.Run("repo error propagates", func(t *testing.T) {
		t.Parallel()

		f := newFx()

		f.cr.On("ListSummariesByPatternID", mock.Anything, testPatternID).Return(nil, errors.New("db error"))

		result, err := f.svc.ListChunks(context.Background(), testPatternID)

		assert.Nil(t, result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "db error")

		f.pr.AssertNotCalled(t, "Get")
		f.cr.AssertExpectations(t)
	})
}

// ---------- PublishJob error ----------

// TestPublishJobError covers the best-effort half of the design: the rows are
// committed, so a queue failure must not fail the request, but it must leave an
// actionable trace. The earlier version of this test used undecorated content,
// which produces no chunks and therefore no jobs — it asserted an empty publish
// list that would have stayed empty with publication deleted outright.
func TestPublishJobError(t *testing.T) {
	t.Parallel()

	f := newFx()
	f.pub.publishErr = errors.New("queue unavailable")

	var logs bytes.Buffer
	svc := patternsvc.New(f.pr, f.er, f.gr, f.tb, f.cr, f.pub, zerolog.New(&logs))

	jobIDs := []uuid.UUID{
		uuid.MustParse("aaaaaaaa-0000-0000-0000-000000000001"),
		uuid.MustParse("bbbbbbbb-0000-0000-0000-000000000002"),
	}

	expectCommittedTx(f.tb)
	f.pr.tx().On("Create", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		args.Get(1).(*patternrepo.Pattern).ID = testPatternID
	}).Return(nil)
	f.cr.tx().On("CreateBatch", mock.Anything, mock.Anything).Return(nil)
	f.er.tx().On("CreatePendingForChunks", mock.Anything, mock.Anything).Return(jobIDs, nil).Once()

	result, err := svc.Create(context.Background(), patternsvc.Input{
		Name:    "chunked-pattern",
		Content: decoratedContent,
	})

	// The write is durable, so the request succeeds.
	require.NoError(t, err)
	require.NotNil(t, result)

	// Publication was attempted for both committed jobs and both failed.
	assert.Equal(t, jobIDs, f.pub.attemptedIDs)
	assert.Empty(t, f.pub.publishedIDs)

	// Each failure names the job, its pattern and the cause as structured
	// fields, so the pending row can be located and re-published. A message
	// that only says publication failed is not actionable.
	out := logs.String()
	assert.Equal(t, 2, strings.Count(out, `"level":"warn"`))
	assert.Equal(t, 2, strings.Count(out, `"pattern_id":"`+testPatternID.String()+`"`))
	for _, id := range jobIDs {
		assert.Contains(t, out, `"job_id":"`+id.String()+`"`)
	}
	assert.Contains(t, out, "queue unavailable")
}
