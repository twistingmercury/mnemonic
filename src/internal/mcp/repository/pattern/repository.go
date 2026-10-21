package pattern

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/twistingmercury/mnemonic/internal/mcp/repository"
)

// Repository defines the pattern reads the MCP tools need.
type Repository interface {
	// Get retrieves the fields get_pattern shows for one pattern. Returns
	// ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*Pattern, error)

	// GetName retrieves only a pattern's name. Returns ErrNotFound if not found.
	GetName(ctx context.Context, id uuid.UUID) (string, error)

	// GetByIDs retrieves ID, name, tags, language and domain for each of ids.
	// Content is not loaded. Missing IDs are silently skipped.
	GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*Pattern, error)
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
	const query = `
		SELECT id, name, content, tags,
			   enrichment_status, enrichment_error, enriched_at
		FROM patterns
		WHERE id = $1
	`

	var p Pattern
	err := r.db.QueryRow(ctx, query, id).Scan(
		&p.ID,
		&p.Name,
		&p.Content,
		&p.Tags,
		&p.EnrichmentStatus,
		&p.EnrichmentError,
		&p.EnrichedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting pattern: %w", err)
	}

	return &p, nil
}

// GetName retrieves a pattern's name by ID.
func (r *pgxRepository) GetName(ctx context.Context, id uuid.UUID) (string, error) {
	var name string
	err := r.db.QueryRow(ctx, `SELECT name FROM patterns WHERE id = $1`, id).Scan(&name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("getting pattern name: %w", err)
	}
	return name, nil
}

// GetByIDs retrieves the metadata of multiple patterns by their IDs.
// Returns only patterns that exist; missing IDs are silently skipped.
func (r *pgxRepository) GetByIDs(ctx context.Context, ids []uuid.UUID) ([]*Pattern, error) {
	if len(ids) == 0 {
		return []*Pattern{}, nil
	}

	const query = `
		SELECT id, name, tags, language, domain
		FROM patterns
		WHERE id = ANY($1)
	`

	rows, err := r.db.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("getting patterns by IDs: %w", err)
	}
	defer rows.Close()

	patterns := make([]*Pattern, 0, len(ids))

	for rows.Next() {
		var p Pattern
		if err := rows.Scan(&p.ID, &p.Name, &p.Tags, &p.Language, &p.Domain); err != nil {
			return nil, fmt.Errorf("getting patterns by IDs: scanning row: %w", err)
		}

		patterns = append(patterns, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getting patterns by IDs: iterating rows: %w", err)
	}

	return patterns, nil
}
