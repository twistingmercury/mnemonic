// Package enrichment runs the enrichment pipeline for the worker: chunk
// embedding, concept extraction, Neo4j graph sync, and recording failures
// with retry scheduling.
package enrichment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/twistingmercury/librarian-enricher/internal/config"
	chunkrepo "github.com/twistingmercury/librarian-enricher/internal/repository/chunk"
	enrichmentjob "github.com/twistingmercury/librarian-enricher/internal/repository/enrichmentjob"
	graphrepo "github.com/twistingmercury/librarian-enricher/internal/repository/graph"
	patternrepo "github.com/twistingmercury/librarian-enricher/internal/repository/pattern"
	openaisvc "github.com/twistingmercury/librarian-enricher/internal/service/openai"
)

// ErrJobFailed is returned by ProcessJob when a pipeline step failed and the
// failure was recorded on the job (and chunk or pattern) for retry. The worker
// acks the message; the returned error wraps the step's cause so the job's
// span and metrics can report it.
var ErrJobFailed = errors.New("enrichment job failed; failure recorded")

// Service defines the worker-facing interface for processing enrichment jobs.
type Service interface {
	// StartJob marks a pending, due job as processing and returns it.
	// Returns enrichmentjob.ErrUnavailable if the job is missing, already
	// started, or waiting out its retry delay.
	StartJob(ctx context.Context, jobID uuid.UUID) (*enrichmentjob.Job, error)

	// ClaimDueJobs marks up to limit jobs that are due for a retry, or that
	// no queue delivery started, as processing and returns them.
	ClaimDueJobs(ctx context.Context, limit int) ([]*enrichmentjob.Job, error)

	// ProcessJob runs the enrichment pipeline for a started job. A chunk job
	// embeds its chunk; the last chunk of a pattern to finish also extracts
	// the pattern's concepts and syncs them to Neo4j. A legacy pattern job
	// does only the extraction and graph sync.
	//
	// On failure at any pipeline step, marks the job as failed with error detail
	// and returns an error wrapping ErrJobFailed and the cause. Any other non-nil
	// error is unrecoverable (e.g., failure to mark the job as failed or to
	// update enrichment status).
	ProcessJob(ctx context.Context, job *enrichmentjob.Job) error

	// ReclaimStaleJobs resets jobs stuck in processing state back to pending.
	// Called periodically by the worker lifecycle manager.
	ReclaimStaleJobs(ctx context.Context) (int64, error)

	// CleanupCompletedJobs removes completed jobs older than the retention period.
	CleanupCompletedJobs(ctx context.Context) (int64, error)

	// CleanupFailedJobs removes failed jobs older than the retention period.
	CleanupFailedJobs(ctx context.Context) (int64, error)
}

// enrichmentService implements the Service interface.
type enrichmentService struct {
	jobRepo       enrichmentjob.Repository
	patternRepo   patternrepo.Repository
	graphRepo     graphrepo.Repository
	chunkRepo     chunkrepo.Repository
	embeddingSvc  openaisvc.EmbeddingService
	extractionSvc openaisvc.ExtractionService
	cfg           config.EnrichmentConfig
	logger        zerolog.Logger
}

// New creates a new enrichment Service backed by the given dependencies.
func New(
	jobRepo enrichmentjob.Repository,
	patternRepo patternrepo.Repository,
	graphRepo graphrepo.Repository,
	embeddingSvc openaisvc.EmbeddingService,
	extractionSvc openaisvc.ExtractionService,
	cfg config.EnrichmentConfig,
	chunkRepo chunkrepo.Repository,
	logger zerolog.Logger,
) Service {
	return &enrichmentService{
		jobRepo:       jobRepo,
		patternRepo:   patternRepo,
		graphRepo:     graphRepo,
		chunkRepo:     chunkRepo,
		embeddingSvc:  embeddingSvc,
		extractionSvc: extractionSvc,
		cfg:           cfg,
		logger:        logger,
	}
}

// StartJob marks a pending, due job as processing and returns it.
func (s *enrichmentService) StartJob(ctx context.Context, jobID uuid.UUID) (*enrichmentjob.Job, error) {
	return s.jobRepo.Start(ctx, jobID)
}

// undeliveredJobGrace is how long a new job may stay pending before the retry
// sweep takes it instead of waiting for its queue message. It covers jobs the
// API saved but failed to publish.
const undeliveredJobGrace = 2 * time.Minute

// ClaimDueJobs claims jobs waiting for a retry, and jobs pending longer than
// undeliveredJobGrace, up to limit.
func (s *enrichmentService) ClaimDueJobs(ctx context.Context, limit int) ([]*enrichmentjob.Job, error) {
	jobs, err := s.jobRepo.ClaimDue(ctx, undeliveredJobGrace, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due jobs: %w", err)
	}
	return jobs, nil
}

// ProcessJob dispatches to the chunk-based or pattern-based pipeline based on
// which ID field the job carries.
func (s *enrichmentService) ProcessJob(ctx context.Context, job *enrichmentjob.Job) error {
	if job.ChunkID != nil {
		return s.processChunkJob(ctx, job)
	}
	return s.processPatternJob(ctx, job)
}

// processChunkJob runs the chunk-based enrichment pipeline:
//  1. Load the chunk with its pattern's name and tags (one query)
//  2. Generate an embedding for "name | tags | section_title" + content
//  3. Store the embedding and mark the chunk enriched (one statement)
//  4. Check the pattern's chunk progress (one query)
//  5. If every chunk is enriched, load the pattern and run the graph pipeline
//  6. Mark the job completed
func (s *enrichmentService) processChunkJob(ctx context.Context, job *enrichmentjob.Job) error {
	chunk, err := s.chunkRepo.Get(ctx, *job.ChunkID)
	if err != nil {
		return s.failJob(ctx, job, fmt.Errorf("load chunk: %w", err))
	}

	// Prepend pattern name, tags (when present), and section title so the embedding
	// captures semantic context beyond the raw code/prose of the section body.
	var embedText string
	if len(chunk.PatternTags) > 0 {
		tags := strings.Join(chunk.PatternTags, ", ")
		embedText = fmt.Sprintf("%s | %s | %s\n\n%s", chunk.PatternName, tags, chunk.SectionTitle, chunk.Content)
	} else {
		embedText = fmt.Sprintf("%s | %s\n\n%s", chunk.PatternName, chunk.SectionTitle, chunk.Content)
	}
	embedding, err := s.embeddingSvc.Embed(ctx, embedText)
	if err != nil {
		return s.failChunkJob(ctx, job, chunk.ID, chunk.PatternID, fmt.Errorf("embed chunk: %w", err))
	}

	if err := s.chunkRepo.MarkEnriched(ctx, chunk.ID, embedding); err != nil {
		return s.failChunkJob(ctx, job, chunk.ID, chunk.PatternID, fmt.Errorf("store chunk embedding: %w", err))
	}

	progress, err := s.chunkRepo.PatternProgress(ctx, chunk.PatternID)
	switch {
	case err != nil:
		// Aggregate query failure: transient DB issue should not block this chunk's
		// successful completion. Log and continue without updating pattern status.
		s.logger.Error().
			Err(err).
			Str("pattern_id", chunk.PatternID.String()).
			Msg("failed to check chunk progress; continuing")
	case progress.AnyFailed:
		// This chunk succeeded; another chunk caused the aggregate failure.
		errMsg := "one or more chunks failed enrichment"
		if updateErr := s.patternRepo.UpdateEnrichmentStatus(ctx, chunk.PatternID, "failed", &errMsg); updateErr != nil {
			s.logger.Error().
				Err(updateErr).
				Str("pattern_id", chunk.PatternID.String()).
				Msg("failed to mark pattern as failed after chunk failure")
		}
	case progress.AllEnriched:
		// The full pattern content is loaded only here, once per pattern,
		// rather than by every chunk job.
		pattern, err := s.patternRepo.Get(ctx, chunk.PatternID)
		if err != nil {
			return s.failJob(ctx, job, fmt.Errorf("load pattern: %w", err))
		}
		// On failure (recorded or not) we must not call MarkCompleted.
		if pipeErr := s.runGraphPipeline(ctx, job, pattern); pipeErr != nil {
			return pipeErr
		}
		if updateErr := s.patternRepo.UpdateEnrichmentStatus(ctx, chunk.PatternID, "enriched", nil); updateErr != nil {
			return fmt.Errorf("update enrichment status: %w", updateErr)
		}
	}

	if err := s.jobRepo.MarkCompleted(ctx, job.ID); err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	return nil
}

// runGraphPipeline extracts concepts from a pattern and syncs the Neo4j graph
// in one write transaction.
//
// On a pipeline step failure it returns failJob's result: ErrJobFailed when
// the failure was recorded, or the unrecoverable recording error.
func (s *enrichmentService) runGraphPipeline(ctx context.Context, job *enrichmentjob.Job, pattern *patternrepo.Pattern) error {
	concepts, err := s.extractionSvc.Extract(ctx, pattern.Content)
	if err != nil {
		return s.failJob(ctx, job, fmt.Errorf("extract concepts: %w", err))
	}

	// openai.Concept and graph.Concept are mapped explicitly: they belong to
	// packages with different responsibilities (extraction vs. graph storage).
	graphConcepts := make([]graphrepo.Concept, len(concepts))
	for i, c := range concepts {
		graphConcepts[i] = graphrepo.Concept{Name: c.Name, Type: c.Type}
	}
	graphPattern := graphrepo.Pattern{ID: pattern.ID, Name: pattern.Name, Description: pattern.Description}
	if err := s.graphRepo.SyncPattern(ctx, graphPattern, graphConcepts, s.cfg.RelatedToMinSimilarity); err != nil {
		return s.failJob(ctx, job, fmt.Errorf("sync graph: %w", err))
	}

	return nil
}

// processPatternJob runs the legacy pattern-level enrichment pipeline for jobs
// where PatternID is set and ChunkID is nil.
func (s *enrichmentService) processPatternJob(ctx context.Context, job *enrichmentjob.Job) error {
	if job.PatternID == nil {
		return s.failJob(ctx, job, fmt.Errorf("load pattern: job has no pattern_id"))
	}

	pattern, err := s.patternRepo.Get(ctx, *job.PatternID)
	if err != nil {
		return s.failJob(ctx, job, fmt.Errorf("load pattern: %w", err))
	}

	if err := s.runGraphPipeline(ctx, job, pattern); err != nil {
		return err
	}

	// Unrecoverable if either of these fails.
	if err := s.patternRepo.UpdateEnrichmentStatus(ctx, *job.PatternID, "enriched", nil); err != nil {
		return fmt.Errorf("update enrichment status: %w", err)
	}
	if err := s.jobRepo.MarkCompleted(ctx, job.ID); err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	return nil
}

// failChunkJob marks both the chunk and its parent pattern as failed, then
// marks the job failed. Use this when a chunk has been loaded (so PatternID
// is known) but a subsequent step fails.
func (s *enrichmentService) failChunkJob(ctx context.Context, job *enrichmentjob.Job, chunkID uuid.UUID, patternID uuid.UUID, cause error) error {
	errMsg := cause.Error()
	if err := s.chunkRepo.MarkFailed(ctx, chunkID, errMsg); err != nil {
		s.logger.Error().
			Err(err).
			Str("chunk_id", chunkID.String()).
			Msg("failed to update chunk enrichment status")
	}
	if err := s.patternRepo.UpdateEnrichmentStatus(ctx, patternID, "failed", &errMsg); err != nil {
		s.logger.Error().
			Err(err).
			Str("pattern_id", patternID.String()).
			Msg("failed to update pattern enrichment status")
	}
	if err := s.jobRepo.MarkFailed(ctx, job.ID, cause, s.cfg.RetryDelay); err != nil {
		return fmt.Errorf("mark job failed: %w (original cause: %v)", err, cause)
	}
	return fmt.Errorf("%w: %w", ErrJobFailed, cause)
}

// failJob marks the job as failed and updates the pattern enrichment status.
// It returns an error wrapping ErrJobFailed and cause when the failure is
// recorded, or the recording error when it cannot be (unrecoverable).
func (s *enrichmentService) failJob(ctx context.Context, job *enrichmentjob.Job, cause error) error {
	// Errors returned here are logged by the worker, so they are not logged
	// twice.
	if err := s.jobRepo.MarkFailed(ctx, job.ID, cause, s.cfg.RetryDelay); err != nil {
		return fmt.Errorf("mark job failed: %w (original cause: %v)", err, cause)
	}

	errMsg := cause.Error()
	if job.PatternID != nil {
		if err := s.patternRepo.UpdateEnrichmentStatus(ctx, *job.PatternID, "failed", &errMsg); err != nil {
			return fmt.Errorf("update enrichment status: %w (original cause: %v)", err, cause)
		}
	}

	return fmt.Errorf("%w: %w", ErrJobFailed, cause)
}

// ReclaimStaleJobs resets jobs stuck in processing state back to pending.
func (s *enrichmentService) ReclaimStaleJobs(ctx context.Context) (int64, error) {
	count, err := s.jobRepo.ReclaimStale(ctx, s.cfg.JobTimeout)
	if err != nil {
		return 0, fmt.Errorf("reclaim stale jobs: %w", err)
	}
	return count, nil
}

// CleanupCompletedJobs removes completed jobs older than the retention period.
func (s *enrichmentService) CleanupCompletedJobs(ctx context.Context) (int64, error) {
	count, err := s.jobRepo.DeleteCompleted(ctx, s.cfg.CompletedRetention)
	if err != nil {
		return 0, fmt.Errorf("cleanup completed jobs: %w", err)
	}
	return count, nil
}

// CleanupFailedJobs removes failed jobs older than the retention period.
func (s *enrichmentService) CleanupFailedJobs(ctx context.Context) (int64, error) {
	count, err := s.jobRepo.DeleteFailed(ctx, s.cfg.FailedRetention)
	if err != nil {
		return 0, fmt.Errorf("cleanup failed jobs: %w", err)
	}
	return count, nil
}

// Compile-time check that *enrichmentService implements Service.
var _ Service = (*enrichmentService)(nil)
