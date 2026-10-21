// Package pattern provides the business logic layer for pattern lifecycle management.
// It coordinates between the PostgreSQL pattern, enrichment job repositories,
// and the Neo4j graph repository, handling enrichment job creation and
// best-effort graph synchronization.
package pattern

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/librarian/internal/api/queue"
	"github.com/twistingmercury/librarian/internal/api/repository"
	chunkrepo "github.com/twistingmercury/librarian/internal/api/repository/chunk"
	enrichRepo "github.com/twistingmercury/librarian/internal/api/repository/enrichmentjob"
	graphRepo "github.com/twistingmercury/librarian/internal/api/repository/graph"
	patternRepo "github.com/twistingmercury/librarian/internal/api/repository/pattern"
	"github.com/twistingmercury/librarian/internal/api/service"
)

// Service defines the operations for managing pattern lifecycle.
type Service interface {
	// Create stores a pattern with its chunks and one pending enrichment job
	// per chunk, atomically. Enrichment runs out of process afterwards, so a
	// successful Create does not mean the pattern is searchable yet.
	// Returns service.ErrConflict if the pattern name already exists.
	Create(ctx context.Context, input Input) (*patternRepo.Pattern, error)

	// Get retrieves a pattern by ID. Returns service.ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*patternRepo.Pattern, error)

	// GetWithGraph retrieves a pattern and, if enriched, its graph context
	// (related patterns and concepts). Neo4j failures degrade gracefully,
	// returning (pattern, nil, nil) instead of an error.
	GetWithGraph(ctx context.Context, id uuid.UUID) (*patternRepo.Pattern, *GraphContext, error)

	// Update modifies an existing pattern, creates a new enrichment job
	Update(ctx context.Context, id uuid.UUID, input Input) (*patternRepo.Pattern, error)

	// Delete removes a pattern from Postgres (CASCADE handles associations and
	// jobs) and best-effort cleans up Neo4j.
	Delete(ctx context.Context, id uuid.UUID) error

	// List retrieves patterns with filtering and pagination.
	List(ctx context.Context, filter patternRepo.Filter, opts ListOptions) ([]*patternRepo.Pattern, error)

	// ListChunks retrieves chunk summaries (no content) for a pattern, ordered
	// by chunk_index. It returns an empty slice when the pattern has no chunks
	// and does not check that the pattern exists.
	ListChunks(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error)
}

// Input contains the client-supplied fields for creating or updating a
// pattern.
type Input struct {
	Name            string
	Description     *string
	Content         string
	Tags            []string
	EntityType      string
	Language        string
	Domain          string
	Version         *string
	RelatedPatterns []string
}

// GraphContext holds the knowledge graph context for a pattern.
type GraphContext struct {
	RelatedPatterns []RelatedPatternResult
	Concepts        []ConceptResult
}

// RelatedPatternResult represents a pattern discovered through shared concepts.
type RelatedPatternResult struct {
	ID             uuid.UUID
	Name           string
	Relationship   string
	Similarity     float64
	SharedConcepts []string
}

// ConceptResult represents a concept linked to a pattern.
type ConceptResult struct {
	Name string
}

// ListOptions defines service-layer pagination parameters.
type ListOptions struct {
	Offset int
	Limit  int
}

// patternService implements the Service interface.
type patternService struct {
	patternRepo    patternRepo.Repository
	enrichmentRepo enrichRepo.Repository
	graphRepo      graphRepo.Repository
	chunkRepo      chunkrepo.Repository
	pool           repository.TxBeginner
	publisher      queue.Publisher
	logger         zerolog.Logger
}

// New creates a new pattern Service backed by the given repositories.
func New(
	patternRepo patternRepo.Repository,
	enrichmentRepo enrichRepo.Repository,
	graphRepo graphRepo.Repository,
	pool repository.TxBeginner,
	chunkRepo chunkrepo.Repository,
	publisher queue.Publisher,
	logger zerolog.Logger,
) Service {
	return &patternService{
		patternRepo:    patternRepo,
		enrichmentRepo: enrichmentRepo,
		graphRepo:      graphRepo,
		chunkRepo:      chunkRepo,
		pool:           pool,
		publisher:      publisher,
		logger:         logger,
	}
}

// chunk is a parsed chunk from content.
type chunk struct {
	Title   string
	Content string
}

// splitIntoChunks parses markdown content and returns a chunk for each section
// preceded by a "[//]: pattern" decorator line. The heading following the
// decorator becomes the chunk title; all subsequent lines until the next
// decorator (or EOF) form the body. Lines outside decorated sections are
// discarded. Content with no decorated section becomes a single chunk, so
// every pattern gets an enrichment job instead of staying pending forever.
func splitIntoChunks(content string) []chunk {
	var chunks []chunk
	var currentTitle string
	var currentLines []string
	pendingPattern := false

	flush := func() {
		if currentTitle == "" {
			return
		}
		body := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if body != "" {
			chunks = append(chunks, chunk{Title: currentTitle, Content: body})
		}
		currentTitle = ""
		currentLines = nil
	}

	for line := range strings.SplitSeq(content, "\n") {
		if line == "[//]: pattern" {
			flush()
			pendingPattern = true
		} else if pendingPattern && strings.HasPrefix(line, "#") {
			title := strings.TrimSpace(strings.TrimLeft(line, "#"))
			currentTitle = title
			currentLines = nil
			pendingPattern = false
		} else if currentTitle != "" {
			currentLines = append(currentLines, line)
		}
		// else: outside any decorated section — discard line
	}
	flush()

	if len(chunks) == 0 {
		if body := strings.TrimSpace(content); body != "" {
			chunks = append(chunks, chunk{Title: firstHeading(body), Content: body})
		}
	}

	return chunks
}

// firstHeading returns the text of the first markdown heading in content, or
// "Content" when there is none.
func firstHeading(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, "#") {
			if title := strings.TrimSpace(strings.TrimLeft(line, "#")); title != "" {
				return title
			}
		}
	}
	return "Content"
}

// publishJob enqueues jobID for async enrichment. Errors are best-effort:
// the job row already exists in PostgreSQL so the pattern is never lost.
func (s *patternService) publishJob(ctx context.Context, jobID, patternID uuid.UUID) {
	if err := s.publisher.Publish(ctx, jobID); err != nil {
		s.logger.Warn().
			Err(err).
			Str("job_id", jobID.String()).
			Str("pattern_id", patternID.String()).
			Msg("failed to publish enrichment job — job remains pending in postgres")
	}
}

// Create writes the pattern, its chunks and their enrichment jobs in one
// transaction, then publishes the jobs. Publication is best-effort: a failure
// there leaves the job rows pending for recovery rather than losing the work.
func (s *patternService) Create(ctx context.Context, input Input) (*patternRepo.Pattern, error) {
	pattern := patternRepo.Pattern{
		Name:            input.Name,
		Description:     input.Description,
		Content:         input.Content,
		Tags:            input.Tags,
		EntityType:      input.EntityType,
		Language:        input.Language,
		Domain:          input.Domain,
		Version:         input.Version,
		RelatedPatterns: input.RelatedPatterns,
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := s.patternRepo.WithTx(tx).Create(ctx, &pattern); err != nil {
		if errors.Is(err, patternRepo.ErrNameExists) {
			return nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, input.Name)
		}
		return nil, fmt.Errorf("create pattern: %w", err)
	}

	jobIDs, err := s.insertChunksAndJobs(ctx, tx, pattern.ID, input.Content)
	if err != nil {
		return nil, fmt.Errorf("failed insert chunks and jobs: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	for _, id := range jobIDs {
		s.publishJob(ctx, id, pattern.ID)
	}

	return &pattern, nil
}

// Get retrieves a pattern by ID.
func (s *patternService) Get(ctx context.Context, id uuid.UUID) (*patternRepo.Pattern, error) {
	pattern, err := s.patternRepo.Get(ctx, id)
	if err != nil {
		if errors.Is(err, patternRepo.ErrNotFound) {
			return nil, fmt.Errorf("%w: pattern %s", service.ErrNotFound, id)
		}
		return nil, fmt.Errorf("get pattern: %w", err)
	}
	return pattern, nil
}

// GetWithGraph retrieves a pattern and its graph context. If the pattern is not
// enriched or Neo4j is unavailable, the graph context is nil.
func (s *patternService) GetWithGraph(ctx context.Context, id uuid.UUID) (*patternRepo.Pattern, *GraphContext, error) {
	pattern, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	if pattern.EnrichmentStatus != "enriched" {
		return pattern, nil, nil
	}

	// Fetch graph context; degrade gracefully on Neo4j failure.
	graphCtx, err := s.fetchGraphContext(ctx, id)
	if err != nil {
		s.logger.Warn().
			Err(err).
			Str("pattern_id", id.String()).
			Msg("failed to fetch graph context, returning without graph")
		return pattern, nil, nil
	}

	return pattern, graphCtx, nil
}

// Update rewrites the pattern and replaces every chunk and enrichment job in
// one transaction, then publishes the new jobs. Deleting the old chunks
// cascades their jobs away, so a partial update would strand the pattern with
// no route back to being enriched.
func (s *patternService) Update(ctx context.Context, patternID uuid.UUID, input Input) (*patternRepo.Pattern, error) {
	existing, err := s.Get(ctx, patternID)
	if err != nil {
		return nil, err
	}

	// Build updated pattern preserving the ID.
	existing.Name = input.Name
	existing.Description = input.Description
	existing.Content = input.Content
	existing.Tags = input.Tags
	existing.EntityType = input.EntityType
	existing.Language = input.Language
	existing.Domain = input.Domain
	existing.Version = input.Version
	existing.RelatedPatterns = input.RelatedPatterns

	jobIDs, err := s.updateWithTransaction(ctx, existing)
	if err != nil {
		return nil, err
	}

	for _, jobId := range jobIDs {
		s.publishJob(ctx, jobId, patternID)
	}

	return existing, nil
}

func (s *patternService) updateWithTransaction(
	ctx context.Context,
	existing *patternRepo.Pattern,
) ([]uuid.UUID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("update pattern: begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// construct tx-scoped repos so all writes participate in the same ransaction.
	txPatternRepo := s.patternRepo.WithTx(tx)
	txChunkRepo := s.chunkRepo.WithTx(tx)

	// Step 1: update the pattern row.
	if err = txPatternRepo.Update(ctx, existing); err != nil {
		if errors.Is(err, patternRepo.ErrNameExists) {
			return nil, fmt.Errorf("%w: pattern %q", service.ErrConflict, existing.Name)
		}
		return nil, fmt.Errorf("update pattern: %w", err)
	}

	// Step 2: delete stale chunks (cascades to their enrichment jobs via ON DELETE CASCADE).
	if err = txChunkRepo.DeleteByPatternID(ctx, existing.ID); err != nil {
		return nil, fmt.Errorf("update pattern: delete stale chunks: %w", err)
	}

	// Step 3: re-split, insert new chunks, and create their enrichment jobs.
	jobIDs, err := s.insertChunksAndJobs(ctx, tx, existing.ID, existing.Content)
	if err != nil {
		return nil, fmt.Errorf("update pattern: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("update pattern: commit transaction: %w", err)
	}

	return jobIDs, nil
}

// Delete removes a pattern from Postgres and best-effort cleans up Neo4j.
func (s *patternService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.patternRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, patternRepo.ErrNotFound) {
			return fmt.Errorf("%w: pattern %s", service.ErrNotFound, id)
		}
		return fmt.Errorf("delete pattern: %w", err)
	}

	// Best-effort Neo4j cleanup; DeletePattern also drops concepts left orphaned.
	if err := s.graphRepo.DeletePattern(ctx, id); err != nil {
		s.logger.Warn().
			Err(err).
			Str("entity", "pattern:delete:"+id.String()).
			Msg("neo4j sync failed")
	}

	return nil
}

// List retrieves patterns with filtering and pagination.
func (s *patternService) List(ctx context.Context, filter patternRepo.Filter, opts ListOptions) ([]*patternRepo.Pattern, error) {
	patterns, err := s.patternRepo.List(ctx, filter, repository.ListOptions{
		Offset: opts.Offset,
		Limit:  opts.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list patterns: %w", err)
	}
	return patterns, nil
}

// ListChunks retrieves chunk summaries for a pattern, ordered by chunk_index.
// It does not check that the pattern exists; callers that need a 404 already
// load the pattern first, so a second lookup here would only repeat that read.
func (s *patternService) ListChunks(ctx context.Context, patternID uuid.UUID) ([]*chunkrepo.Chunk, error) {
	chunks, err := s.chunkRepo.ListSummariesByPatternID(ctx, patternID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	return chunks, nil
}

// fetchGraphContext retrieves related patterns and concepts from Neo4j in a
// single session.
func (s *patternService) fetchGraphContext(ctx context.Context, patternID uuid.UUID) (*GraphContext, error) {
	graph, err := s.graphRepo.GetPatternGraph(ctx, patternID, 10)
	if err != nil {
		return nil, fmt.Errorf("fetching graph context: %w", err)
	}
	related, concepts := graph.RelatedPatterns, graph.Concepts

	relatedResults := make([]RelatedPatternResult, len(related))
	for i, r := range related {
		relatedResults[i] = RelatedPatternResult{
			ID:             r.ID,
			Name:           r.Name,
			Relationship:   "RELATED_TO",
			Similarity:     r.Similarity,
			SharedConcepts: r.ConceptNames,
		}
	}

	conceptResults := make([]ConceptResult, len(concepts))
	for i, c := range concepts {
		conceptResults[i] = ConceptResult{Name: c.Name}
	}

	return &GraphContext{
		RelatedPatterns: relatedResults,
		Concepts:        conceptResults,
	}, nil
}

// insertChunksAndJobs inserts the chunks for content and one pending job per
// chunk, returning the job IDs. Each set is one statement, so this costs two
// round trips however many chunks the content has. Publish the jobs only after
// the caller commits: a message sent inside the transaction can outlive a
// rollback, leaving the enricher to claim a job row that no longer exists.
func (s *patternService) insertChunksAndJobs(ctx context.Context, tx repository.DBTX, patternID uuid.UUID, content string) ([]uuid.UUID, error) {
	chunkRepo := s.chunkRepo.WithTx(tx)
	jobRepo := s.enrichmentRepo.WithTx(tx)

	rawChunks := splitIntoChunks(content)
	if len(rawChunks) == 0 {
		return []uuid.UUID{}, nil
	}

	chunks := make([]*chunkrepo.Chunk, len(rawChunks))
	for i, rc := range rawChunks {
		chunks[i] = &chunkrepo.Chunk{
			PatternID:    patternID,
			SectionTitle: rc.Title,
			ChunkIndex:   i,
			Content:      rc.Content,
		}
	}

	if err := chunkRepo.CreateBatch(ctx, chunks); err != nil {
		return nil, fmt.Errorf("failed to create a chunk batch: %w", err)
	}

	chunkIDs := make([]uuid.UUID, len(chunks))
	for i, c := range chunks {
		chunkIDs[i] = c.ID
	}

	jobIDs, err := jobRepo.CreatePendingForChunks(ctx, chunkIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create enrichment jobs for patternID %v: %w", patternID, err)
	}

	return jobIDs, nil
}
