package chunk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
	"github.com/twistingmercury/librarian/internal/enricher/repository"
)

// Repository defines the chunk operations the enrichment pipeline needs.
type Repository interface {
	// Get retrieves a chunk and its parent pattern's name and tags in one
	// query. Returns ErrNotFound when absent.
	Get(ctx context.Context, id uuid.UUID) (*Chunk, error)

	// MarkEnriched stores the chunk's embedding and sets its status to
	// "enriched" in one statement.
	MarkEnriched(ctx context.Context, id uuid.UUID, embedding []float32) error

	// MarkFailed sets the chunk's status to "failed" with errMsg.
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error

	// PatternProgress reports whether any of a pattern's chunks failed and
	// whether all of them are enriched, in one query.
	PatternProgress(ctx context.Context, patternID uuid.UUID) (Progress, error)
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Get retrieves a chunk joined to its parent pattern's name and tags.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*Chunk, error) {
	query := `
		SELECT pc.id, pc.pattern_id, pc.section_title, pc.content, p.name, p.tags
		FROM pattern_chunks pc
		JOIN patterns p ON p.id = pc.pattern_id
		WHERE pc.id = $1
	`

	var c Chunk
	var tagsJSON []byte
	err := r.db.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.PatternID, &c.SectionTitle, &c.Content, &c.PatternName, &tagsJSON,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting chunk: %w", err)
	}

	if err := json.Unmarshal(tagsJSON, &c.PatternTags); err != nil {
		return nil, fmt.Errorf("getting chunk: unmarshaling pattern tags: %w", err)
	}

	return &c, nil
}

// MarkEnriched stores the embedding and marks the chunk enriched.
func (r *pgxRepository) MarkEnriched(ctx context.Context, id uuid.UUID, embedding []float32) error {
	query := `
		UPDATE pattern_chunks SET
			embedding = $2,
			enrichment_status = 'enriched',
			enrichment_error = NULL,
			enriched_at = now(),
			updated_at = now()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, pgvector.NewVector(embedding))
	if err != nil {
		return fmt.Errorf("marking chunk enriched: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// MarkFailed marks the chunk failed with errMsg.
func (r *pgxRepository) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	query := `
		UPDATE pattern_chunks SET
			enrichment_status = 'failed',
			enrichment_error = $2,
			enriched_at = NULL,
			updated_at = now()
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, errMsg)
	if err != nil {
		return fmt.Errorf("marking chunk failed: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// PatternProgress aggregates a pattern's chunk statuses in one query.
func (r *pgxRepository) PatternProgress(ctx context.Context, patternID uuid.UUID) (Progress, error) {
	query := `
		SELECT COUNT(*) FILTER (WHERE enrichment_status = 'failed') > 0,
		       COUNT(*) > 0 AND COUNT(*) FILTER (WHERE enrichment_status != 'enriched') = 0
		FROM pattern_chunks
		WHERE pattern_id = $1
	`

	var p Progress
	if err := r.db.QueryRow(ctx, query, patternID).Scan(&p.AnyFailed, &p.AllEnriched); err != nil {
		return Progress{}, fmt.Errorf("checking chunk progress for pattern: %w", err)
	}

	return p, nil
}
