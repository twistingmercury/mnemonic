package pattern

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/twistingmercury/mnemonic/internal/enricher/repository"
)

// Repository defines the pattern operations the enrichment pipeline needs.
type Repository interface {
	// Get retrieves a pattern by ID. Returns ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*Pattern, error)

	// UpdateEnrichmentStatus updates the enrichment state of a pattern.
	UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Get retrieves a pattern by ID from the database.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*Pattern, error) {
	query := `SELECT id, name, description, content FROM patterns WHERE id = $1`

	var p Pattern
	err := r.db.QueryRow(ctx, query, id).Scan(&p.ID, &p.Name, &p.Description, &p.Content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting pattern: %w", err)
	}

	return &p, nil
}

// UpdateEnrichmentStatus updates the enrichment state of a pattern. When
// status is "enriched", enriched_at is set to the current time.
func (r *pgxRepository) UpdateEnrichmentStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error {
	now := time.Now()

	var enrichedAt *time.Time
	if status == "enriched" {
		enrichedAt = &now
	}

	query := `
		UPDATE patterns SET
			enrichment_status = $2,
			enrichment_error = $3,
			enriched_at = $4,
			updated_at = $5
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query, id, status, errMsg, enrichedAt, now)
	if err != nil {
		return fmt.Errorf("updating pattern enrichment status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}
