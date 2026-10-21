package enrichment_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/enricher/config"
	chunkrepo "github.com/twistingmercury/librarian/internal/enricher/repository/chunk"
	enrichmentjob "github.com/twistingmercury/librarian/internal/enricher/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/librarian/internal/enricher/repository/graph"
	patternrepo "github.com/twistingmercury/librarian/internal/enricher/repository/pattern"
	"github.com/twistingmercury/librarian/internal/enricher/service/enrichment"
	openaisvc "github.com/twistingmercury/librarian/internal/enricher/service/openai"
)

// --- Mock: enrichmentjob.Repository ---

type mockJobRepo struct {
	mock.Mock
}

func (m *mockJobRepo) Start(ctx context.Context, id uuid.UUID) (*enrichmentjob.Job, error) {
	args := m.Called(ctx, id)
	job, _ := args.Get(0).(*enrichmentjob.Job)
	return job, args.Error(1)
}

func (m *mockJobRepo) ClaimDue(ctx context.Context, grace time.Duration, limit int) ([]*enrichmentjob.Job, error) {
	args := m.Called(ctx, grace, limit)
	jobs, _ := args.Get(0).([]*enrichmentjob.Job)
	return jobs, args.Error(1)
}

func (m *mockJobRepo) MarkCompleted(ctx context.Context, id uuid.UUID) error {
	return m.Called(ctx, id).Error(0)
}

func (m *mockJobRepo) MarkFailed(ctx context.Context, id uuid.UUID, err error, retryDelay time.Duration) error {
	return m.Called(ctx, id, err, retryDelay).Error(0)
}

func (m *mockJobRepo) ReclaimStale(ctx context.Context, timeout time.Duration) (int64, error) {
	args := m.Called(ctx, timeout)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockJobRepo) DeleteCompleted(ctx context.Context, retention time.Duration) (int64, error) {
	args := m.Called(ctx, retention)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockJobRepo) DeleteFailed(ctx context.Context, retention time.Duration) (int64, error) {
	args := m.Called(ctx, retention)
	return args.Get(0).(int64), args.Error(1)
}

// --- Mock: patternrepo.Repository ---

type mockPatternRepo struct {
	mock.Mock
}

func (m *mockPatternRepo) Get(ctx context.Context, id uuid.UUID) (*patternrepo.Pattern, error) {
	args := m.Called(ctx, id)
	p, _ := args.Get(0).(*patternrepo.Pattern)
	return p, args.Error(1)
}

func (m *mockPatternRepo) UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	return m.Called(ctx, id, status, errMsg).Error(0)
}

// --- Mock: graphrepo.Repository ---

type mockGraphRepo struct {
	mock.Mock
}

func (m *mockGraphRepo) SyncPattern(ctx context.Context, pattern graphrepo.Pattern, concepts []graphrepo.Concept, minSimilarity float64) error {
	return m.Called(ctx, pattern, concepts, minSimilarity).Error(0)
}

// --- Mock: chunkrepo.Repository ---

type mockChunkRepo struct {
	mock.Mock
}

func (m *mockChunkRepo) Get(ctx context.Context, id uuid.UUID) (*chunkrepo.Chunk, error) {
	args := m.Called(ctx, id)
	c, _ := args.Get(0).(*chunkrepo.Chunk)
	return c, args.Error(1)
}

func (m *mockChunkRepo) MarkEnriched(ctx context.Context, id uuid.UUID, embedding []float32) error {
	return m.Called(ctx, id, embedding).Error(0)
}

func (m *mockChunkRepo) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	return m.Called(ctx, id, errMsg).Error(0)
}

func (m *mockChunkRepo) PatternProgress(ctx context.Context, patternID uuid.UUID) (chunkrepo.Progress, error) {
	args := m.Called(ctx, patternID)
	return args.Get(0).(chunkrepo.Progress), args.Error(1)
}

// --- Mock: openaisvc.EmbeddingService ---

type mockEmbeddingSvc struct {
	mock.Mock
}

func (m *mockEmbeddingSvc) Embed(ctx context.Context, text string) ([]float32, error) {
	args := m.Called(ctx, text)
	e, _ := args.Get(0).([]float32)
	return e, args.Error(1)
}

// --- Mock: openaisvc.ExtractionService ---

type mockExtractionSvc struct {
	mock.Mock
}

func (m *mockExtractionSvc) Extract(ctx context.Context, text string) ([]openaisvc.Concept, error) {
	args := m.Called(ctx, text)
	c, _ := args.Get(0).([]openaisvc.Concept)
	return c, args.Error(1)
}

// --- Test Fixtures ---

func testConfig() config.EnrichmentConfig {
	return config.EnrichmentConfig{
		WorkerCount:            2,
		RetryDelay:             30 * time.Second,
		JobTimeout:             5 * time.Minute,
		CompletedRetention:     168 * time.Hour,
		FailedRetention:        720 * time.Hour,
		RelatedToMinSimilarity: 0.3,
	}
}

var (
	testPatternID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testJobID     = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	testChunkID   = uuid.MustParse("44444444-4444-4444-4444-444444444444")
	errDB         = errors.New("db error")
)

func testJob() *enrichmentjob.Job {
	pid := testPatternID
	return &enrichmentjob.Job{ID: testJobID, PatternID: &pid}
}

func testChunkJob() *enrichmentjob.Job {
	cid := testChunkID
	return &enrichmentjob.Job{ID: testJobID, ChunkID: &cid}
}

func testPattern() *patternrepo.Pattern {
	desc := "A test pattern"
	return &patternrepo.Pattern{
		ID:          testPatternID,
		Name:        "test-pattern",
		Description: &desc,
		Content:     "This is test pattern content for enrichment.",
	}
}

func testGraphPattern() graphrepo.Pattern {
	p := testPattern()
	return graphrepo.Pattern{ID: p.ID, Name: p.Name, Description: p.Description}
}

func testChunk() *chunkrepo.Chunk {
	return &chunkrepo.Chunk{
		ID:           testChunkID,
		PatternID:    testPatternID,
		SectionTitle: "Test Section",
		Content:      "Test chunk content for embedding.",
		PatternName:  "test-pattern",
		PatternTags:  []string{"test"},
	}
}

// testEmbedText is the text processChunkJob embeds for testChunk():
// "name | tags | section", a blank line, then the content.
const testEmbedText = "test-pattern | test | Test Section\n\nTest chunk content for embedding."

func testEmbedding() []float32 {
	return []float32{0.1, 0.2, 0.3}
}

func testConcepts() []openaisvc.Concept {
	return []openaisvc.Concept{
		{Name: "error handling", Type: "domain"},
		{Name: "go", Type: "technology"},
	}
}

func testGraphConcepts() []graphrepo.Concept {
	return []graphrepo.Concept{
		{Name: "error handling", Type: "domain"},
		{Name: "go", Type: "technology"},
	}
}

type testDeps struct {
	jobRepo       *mockJobRepo
	patternRepo   *mockPatternRepo
	graphRepo     *mockGraphRepo
	embeddingSvc  *mockEmbeddingSvc
	extractionSvc *mockExtractionSvc
	chunkRepo     *mockChunkRepo
}

func newTestService(t *testing.T) (enrichment.Service, *testDeps) {
	t.Helper()
	deps := &testDeps{
		jobRepo:       new(mockJobRepo),
		patternRepo:   new(mockPatternRepo),
		graphRepo:     new(mockGraphRepo),
		embeddingSvc:  new(mockEmbeddingSvc),
		extractionSvc: new(mockExtractionSvc),
		chunkRepo:     new(mockChunkRepo),
	}

	svc := enrichment.New(
		deps.jobRepo,
		deps.patternRepo,
		deps.graphRepo,
		deps.embeddingSvc,
		deps.extractionSvc,
		testConfig(),
		deps.chunkRepo,
		zerolog.Nop(),
	)

	return svc, deps
}

// assertExpectations checks all mock expectations.
func assertExpectations(t *testing.T, deps *testDeps) {
	t.Helper()
	deps.jobRepo.AssertExpectations(t)
	deps.patternRepo.AssertExpectations(t)
	deps.graphRepo.AssertExpectations(t)
	deps.embeddingSvc.AssertExpectations(t)
	deps.extractionSvc.AssertExpectations(t)
	deps.chunkRepo.AssertExpectations(t)
}

// setupGraphMocks wires a successful extraction and graph sync for testPattern.
func setupGraphMocks(deps *testDeps) {
	deps.extractionSvc.On("Extract", mock.Anything, testPattern().Content).Return(testConcepts(), nil)
	deps.graphRepo.On("SyncPattern", mock.Anything, testGraphPattern(), testGraphConcepts(), 0.3).Return(nil)
}

// setupFailJobMocks configures mocks for a successful failJob call on a job
// that carries a PatternID.
func setupFailJobMocks(deps *testDeps) {
	deps.jobRepo.On("MarkFailed", mock.Anything, testJobID, mock.Anything, 30*time.Second).Return(nil)
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "failed", mock.AnythingOfType("*string")).Return(nil)
}

// setupFailChunkJobMocks configures mocks for a successful failChunkJob call.
func setupFailChunkJobMocks(deps *testDeps) {
	deps.chunkRepo.On("MarkFailed", mock.Anything, testChunkID, mock.AnythingOfType("string")).Return(nil)
	setupFailJobMocks(deps)
}

// setupChunkEnrichedMocks wires a chunk job through load, embed, MarkEnriched
// and PatternProgress, returning progress and progressErr from the last step.
func setupChunkEnrichedMocks(deps *testDeps, chunk *chunkrepo.Chunk, embedText string, progress chunkrepo.Progress, progressErr error) {
	deps.chunkRepo.On("Get", mock.Anything, testChunkID).Return(chunk, nil)
	deps.embeddingSvc.On("Embed", mock.Anything, embedText).Return(testEmbedding(), nil)
	deps.chunkRepo.On("MarkEnriched", mock.Anything, testChunkID, testEmbedding()).Return(nil)
	deps.chunkRepo.On("PatternProgress", mock.Anything, testPatternID).Return(progress, progressErr)
}

// assertNoPatternWork checks that the pattern-level part of a chunk job was
// skipped: the full pattern is not loaded and no graph work runs.
func assertNoPatternWork(t *testing.T, deps *testDeps) {
	t.Helper()
	deps.patternRepo.AssertNotCalled(t, "Get", mock.Anything, mock.Anything)
	deps.extractionSvc.AssertNotCalled(t, "Extract", mock.Anything, mock.Anything)
	deps.graphRepo.AssertNotCalled(t, "SyncPattern", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// ---------- StartJob ----------

func TestStartJob(t *testing.T) {
	t.Parallel()

	t.Run("returns started job", func(t *testing.T) {
		t.Parallel()

		svc, deps := newTestService(t)
		deps.jobRepo.On("Start", mock.Anything, testJobID).Return(testJob(), nil)

		result, err := svc.StartJob(context.Background(), testJobID)

		require.NoError(t, err)
		assert.Equal(t, testJob(), result)
		assertExpectations(t, deps)
	})

	t.Run("propagates unavailable error", func(t *testing.T) {
		t.Parallel()

		svc, deps := newTestService(t)
		deps.jobRepo.On("Start", mock.Anything, testJobID).Return(nil, enrichmentjob.ErrUnavailable)

		result, err := svc.StartJob(context.Background(), testJobID)

		require.ErrorIs(t, err, enrichmentjob.ErrUnavailable)
		assert.Nil(t, result)
		assertExpectations(t, deps)
	})
}

// ---------- ClaimDueJobs ----------

func TestClaimDueJobs(t *testing.T) {
	t.Parallel()

	t.Run("claims with a grace period for undelivered jobs", func(t *testing.T) {
		t.Parallel()

		svc, deps := newTestService(t)
		jobs := []*enrichmentjob.Job{testChunkJob()}
		deps.jobRepo.On("ClaimDue", mock.Anything, 2*time.Minute, 3).Return(jobs, nil)

		got, err := svc.ClaimDueJobs(context.Background(), 3)

		require.NoError(t, err)
		assert.Equal(t, jobs, got)
		assertExpectations(t, deps)
	})

	t.Run("wraps repository errors", func(t *testing.T) {
		t.Parallel()

		svc, deps := newTestService(t)
		deps.jobRepo.On("ClaimDue", mock.Anything, mock.Anything, 1).Return(nil, errors.New("db down"))

		_, err := svc.ClaimDueJobs(context.Background(), 1)

		require.ErrorContains(t, err, "claim due jobs: db down")
		assertExpectations(t, deps)
	})
}

// ---------- ProcessJob (legacy pattern-level job) ----------

func TestProcessJob_HappyPath(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	setupGraphMocks(deps)
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "enriched", (*string)(nil)).Return(nil)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(nil)

	err := svc.ProcessJob(context.Background(), testJob())

	require.NoError(t, err)
	assertExpectations(t, deps)
}

func TestProcessJob_LoadPatternFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)
	setupFailJobMocks(deps)

	err := svc.ProcessJob(context.Background(), testJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	require.ErrorIs(t, err, patternrepo.ErrNotFound)
	assertExpectations(t, deps)
}

func TestProcessJob_ExtractionFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	deps.extractionSvc.On("Extract", mock.Anything, testPattern().Content).Return(nil, errors.New("extraction failed"))
	setupFailJobMocks(deps)

	err := svc.ProcessJob(context.Background(), testJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	assertExpectations(t, deps)
}

func TestProcessJob_SyncPatternFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	deps.extractionSvc.On("Extract", mock.Anything, testPattern().Content).Return(testConcepts(), nil)
	deps.graphRepo.On("SyncPattern", mock.Anything, testGraphPattern(), testGraphConcepts(), 0.3).
		Return(errors.New("neo4j unavailable"))
	setupFailJobMocks(deps)

	err := svc.ProcessJob(context.Background(), testJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	assert.Contains(t, err.Error(), "sync graph")
	assertExpectations(t, deps)
}

func TestProcessJob_MarkCompletedFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	setupGraphMocks(deps)
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "enriched", (*string)(nil)).Return(nil)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(errDB)

	err := svc.ProcessJob(context.Background(), testJob())

	require.Error(t, err, "unrecoverable failure should return non-nil error")
	require.NotErrorIs(t, err, enrichment.ErrJobFailed, "an unrecorded failure must not look recorded")
	assert.Contains(t, err.Error(), "mark job completed")
	assertExpectations(t, deps)
}

func TestProcessJob_UpdateEnrichmentStatusFails(t *testing.T) {
	t.Parallel()

	// UpdateEnrichmentStatus runs before MarkCompleted, so MarkCompleted is never reached.
	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	setupGraphMocks(deps)
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "enriched", (*string)(nil)).Return(errDB)

	err := svc.ProcessJob(context.Background(), testJob())

	require.Error(t, err, "unrecoverable failure should return non-nil error")
	require.NotErrorIs(t, err, enrichment.ErrJobFailed, "an unrecorded failure must not look recorded")
	assert.Contains(t, err.Error(), "update enrichment status")
	assertExpectations(t, deps)
}

// ---------- failJob ----------

func TestFailJob_MarkFailedFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)
	deps.jobRepo.On("MarkFailed", mock.Anything, testJobID, mock.Anything, 30*time.Second).Return(errDB)

	err := svc.ProcessJob(context.Background(), testJob())

	require.Error(t, err, "should return error when MarkFailed fails")
	require.NotErrorIs(t, err, enrichment.ErrJobFailed)
	assert.Contains(t, err.Error(), "mark job failed")
	assertExpectations(t, deps)
}

func TestFailJob_UpdateEnrichmentStatusFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(nil, patternrepo.ErrNotFound)
	deps.jobRepo.On("MarkFailed", mock.Anything, testJobID, mock.Anything, 30*time.Second).Return(nil)
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "failed", mock.AnythingOfType("*string")).
		Return(errDB)

	err := svc.ProcessJob(context.Background(), testJob())

	require.Error(t, err, "should return error when UpdateEnrichmentStatus fails in failJob")
	require.NotErrorIs(t, err, enrichment.ErrJobFailed)
	assert.Contains(t, err.Error(), "update enrichment status")
	assertExpectations(t, deps)
}

// ---------- ProcessJob: chunk-based pipeline ----------

func TestProcessJob_ChunkJob_HappyPath_AllEnriched(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, testChunk(), testEmbedText, chunkrepo.Progress{AllEnriched: true}, nil)
	// Last chunk of the pattern: load the pattern, extract concepts, sync the graph.
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	setupGraphMocks(deps)
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "enriched", (*string)(nil)).Return(nil)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.NoError(t, err)
	assertExpectations(t, deps)
}

func TestProcessJob_ChunkJob_HappyPath_NotAllEnriched(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, testChunk(), testEmbedText, chunkrepo.Progress{}, nil)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.NoError(t, err)
	assertExpectations(t, deps)
	// Other chunks still pending: the full pattern must not be loaded.
	assertNoPatternWork(t, deps)
	deps.patternRepo.AssertNotCalled(t, "UpdateEnrichmentStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestProcessJob_ChunkJob_HappyPath_NoTags(t *testing.T) {
	t.Parallel()

	// A chunk whose pattern has no tags exercises the tag-omission branch:
	// the embed text is "name | section\n\ncontent" with no blank segment.
	chunk := testChunk()
	chunk.PatternTags = nil
	expected := "test-pattern | Test Section\n\nTest chunk content for embedding."

	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, chunk, expected, chunkrepo.Progress{}, nil)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.NoError(t, err)
	assertExpectations(t, deps)
}

func TestProcessJob_ChunkJob_AnyFailed_PatternMarkedFailed(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, testChunk(), testEmbedText, chunkrepo.Progress{AnyFailed: true}, nil)
	// Pattern is marked failed; job is still completed (this chunk succeeded).
	deps.patternRepo.On("UpdateEnrichmentStatus", mock.Anything, testPatternID, "failed", mock.AnythingOfType("*string")).Return(nil)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.NoError(t, err)
	assertExpectations(t, deps)
	assertNoPatternWork(t, deps)
}

func TestProcessJob_ChunkJob_PatternProgressFails(t *testing.T) {
	t.Parallel()

	// A failed progress query is logged; this chunk's job still completes.
	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, testChunk(), testEmbedText, chunkrepo.Progress{}, errDB)
	deps.jobRepo.On("MarkCompleted", mock.Anything, testJobID).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.NoError(t, err)
	assertExpectations(t, deps)
	assertNoPatternWork(t, deps)
	deps.patternRepo.AssertNotCalled(t, "UpdateEnrichmentStatus", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestProcessJob_ChunkJob_LoadChunkFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.chunkRepo.On("Get", mock.Anything, testChunkID).Return(nil, chunkrepo.ErrNotFound)
	// failJob: the job has no PatternID, so only MarkFailed is called.
	deps.jobRepo.On("MarkFailed", mock.Anything, testJobID, mock.Anything, 30*time.Second).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	assertExpectations(t, deps)
}

func TestProcessJob_ChunkJob_EmbeddingFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	chunk := testChunk()
	deps.chunkRepo.On("Get", mock.Anything, testChunkID).Return(chunk, nil)
	deps.embeddingSvc.On("Embed", mock.Anything, testEmbedText).Return(nil, errors.New("openai unavailable"))
	setupFailChunkJobMocks(deps)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	assertExpectations(t, deps)
}

func TestProcessJob_ChunkJob_MarkEnrichedFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	chunk := testChunk()
	deps.chunkRepo.On("Get", mock.Anything, testChunkID).Return(chunk, nil)
	deps.embeddingSvc.On("Embed", mock.Anything, testEmbedText).Return(testEmbedding(), nil)
	deps.chunkRepo.On("MarkEnriched", mock.Anything, testChunkID, testEmbedding()).Return(errDB)
	setupFailChunkJobMocks(deps)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	require.ErrorIs(t, err, errDB)
	assertExpectations(t, deps)
	deps.chunkRepo.AssertNotCalled(t, "PatternProgress", mock.Anything, mock.Anything)
}

func TestProcessJob_ChunkJob_AllEnriched_LoadPatternFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, testChunk(), testEmbedText, chunkrepo.Progress{AllEnriched: true}, nil)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(nil, errDB)
	// failJob: the job has no PatternID, so only MarkFailed is called.
	deps.jobRepo.On("MarkFailed", mock.Anything, testJobID, mock.Anything, 30*time.Second).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	assertExpectations(t, deps)
	deps.jobRepo.AssertNotCalled(t, "MarkCompleted", mock.Anything, mock.Anything)
}

func TestProcessJob_ChunkJob_GraphPipelineFails(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	setupChunkEnrichedMocks(deps, testChunk(), testEmbedText, chunkrepo.Progress{AllEnriched: true}, nil)
	deps.patternRepo.On("Get", mock.Anything, testPatternID).Return(testPattern(), nil)
	deps.extractionSvc.On("Extract", mock.Anything, testPattern().Content).Return(nil, errors.New("openai down"))
	// runGraphPipeline calls failJob (not failChunkJob); the chunk job has no
	// PatternID, so failJob only calls MarkFailed.
	deps.jobRepo.On("MarkFailed", mock.Anything, testJobID, mock.Anything, 30*time.Second).Return(nil)

	err := svc.ProcessJob(context.Background(), testChunkJob())

	require.ErrorIs(t, err, enrichment.ErrJobFailed, "a recorded pipeline failure should return ErrJobFailed")
	assertExpectations(t, deps)
	deps.jobRepo.AssertNotCalled(t, "MarkCompleted", mock.Anything, mock.Anything)
}

// ---------- Maintenance ----------

func TestReclaimStaleJobs(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.jobRepo.On("ReclaimStale", mock.Anything, 5*time.Minute).Return(int64(3), nil)

	count, err := svc.ReclaimStaleJobs(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
	assertExpectations(t, deps)
}

func TestReclaimStaleJobs_Error(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.jobRepo.On("ReclaimStale", mock.Anything, 5*time.Minute).Return(int64(0), errDB)

	count, err := svc.ReclaimStaleJobs(context.Background())

	require.Error(t, err)
	assert.Equal(t, int64(0), count)
	assertExpectations(t, deps)
}

func TestCleanupCompletedJobs(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.jobRepo.On("DeleteCompleted", mock.Anything, 168*time.Hour).Return(int64(5), nil)

	count, err := svc.CleanupCompletedJobs(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(5), count)
	assertExpectations(t, deps)
}

func TestCleanupCompletedJobs_Error(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.jobRepo.On("DeleteCompleted", mock.Anything, 168*time.Hour).Return(int64(0), errDB)

	count, err := svc.CleanupCompletedJobs(context.Background())

	require.Error(t, err)
	assert.Equal(t, int64(0), count)
	assertExpectations(t, deps)
}

func TestCleanupFailedJobs(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.jobRepo.On("DeleteFailed", mock.Anything, 720*time.Hour).Return(int64(2), nil)

	count, err := svc.CleanupFailedJobs(context.Background())

	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
	assertExpectations(t, deps)
}

func TestCleanupFailedJobs_Error(t *testing.T) {
	t.Parallel()

	svc, deps := newTestService(t)
	deps.jobRepo.On("DeleteFailed", mock.Anything, 720*time.Hour).Return(int64(0), errDB)

	count, err := svc.CleanupFailedJobs(context.Background())

	require.Error(t, err)
	assert.Equal(t, int64(0), count)
	assertExpectations(t, deps)
}
