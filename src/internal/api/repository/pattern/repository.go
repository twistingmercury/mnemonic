package pattern

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/twistingmercury/librarian/internal/api/repository"
)

// Repository defines data access operations for patterns.
type Repository interface {
	// Create stores a new pattern. Returns ErrNameExists if name already exists.
	Create(ctx context.Context, pattern *Pattern) error

	// Get retrieves a pattern by ID. Returns ErrNotFound if not found.
	Get(ctx context.Context, id uuid.UUID) (*Pattern, error)

	// Update modifies an existing pattern. Returns ErrNotFound if not found.
	Update(ctx context.Context, pattern *Pattern) error

	// Delete removes a pattern by ID. Returns ErrNotFound if not found.
	Delete(ctx context.Context, id uuid.UUID) error

	// List retrieves patterns with filtering and pagination.
	// Returns the patterns with Content left empty.
	List(ctx context.Context, filter Filter, opts repository.ListOptions) ([]*Pattern, error)

	// WithTx returns a Repository that executes all operations within db.
	// Use this to enlist the repository in a caller-owned transaction.
	WithTx(db repository.DBTX) Repository
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// Create stores a new pattern in the database.
func (r *pgxRepository) Create(ctx context.Context, pattern *Pattern) error {
	// Defensive check: ensure Tags is not nil (database requires JSON array, not null)
	if pattern.Tags == nil {
		pattern.Tags = []string{}
	}
	// Defensive check: ensure RelatedPatterns is not nil (database requires JSON array, not null)
	if pattern.RelatedPatterns == nil {
		pattern.RelatedPatterns = []string{}
	}

	tagsJSON, err := json.Marshal(pattern.Tags)
	if err != nil {
		return fmt.Errorf("marshaling tags: %w", err)
	}

	relatedPatternsJSON, err := json.Marshal(pattern.RelatedPatterns)
	if err != nil {
		return fmt.Errorf("marshaling related_patterns: %w", err)
	}

	// Generate UUID if not set
	if pattern.ID == uuid.Nil {
		pattern.ID = uuid.New()
	}

	now := time.Now()
	query := `
		INSERT INTO patterns (
			id, name, description, content, tags,
			entity_type, language, domain, version, related_patterns,
			enrichment_status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = r.db.Exec(ctx, query,
		pattern.ID,
		pattern.Name,
		pattern.Description,
		pattern.Content,
		tagsJSON,
		pattern.EntityType,
		pattern.Language,
		pattern.Domain,
		pattern.Version,
		relatedPatternsJSON,
		"pending", // New patterns start as pending
		now,
		now,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == repository.PgErrCodeUniqueViolation {
			return ErrNameExists
		}
		return fmt.Errorf("creating pattern: %w", err)
	}

	pattern.EnrichmentStatus = "pending"
	pattern.CreatedAt = now
	pattern.UpdatedAt = now
	return nil
}

// Get retrieves a pattern by ID from the database.
func (r *pgxRepository) Get(ctx context.Context, id uuid.UUID) (*Pattern, error) {
	query := `
		SELECT id, name, description, content, tags,
			   entity_type, language, domain, version, related_patterns,
			   enrichment_status, enrichment_error, enriched_at,
			   created_at, updated_at
		FROM patterns
		WHERE id = $1
	`

	var p Pattern
	var tagsJSON []byte
	var relatedPatternsJSON []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&p.ID,
		&p.Name,
		&p.Description,
		&p.Content,
		&tagsJSON,
		&p.EntityType,
		&p.Language,
		&p.Domain,
		&p.Version,
		&relatedPatternsJSON,
		&p.EnrichmentStatus,
		&p.EnrichmentError,
		&p.EnrichedAt,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("getting pattern: %w", err)
	}

	if err := json.Unmarshal(tagsJSON, &p.Tags); err != nil {
		return nil, fmt.Errorf("unmarshaling tags: %w", err)
	}

	if err := json.Unmarshal(relatedPatternsJSON, &p.RelatedPatterns); err != nil {
		return nil, fmt.Errorf("unmarshaling related_patterns: %w", err)
	}

	return &p, nil
}

// Update modifies an existing pattern in the database.
// Per design spec, updating a pattern resets enrichment status to "pending",
// ensuring stale enrichment state does not remain active.
func (r *pgxRepository) Update(ctx context.Context, pattern *Pattern) error {
	// Defensive nil guards: json.Marshal(nil) produces "null" which would violate NOT NULL constraints.
	if pattern.Tags == nil {
		pattern.Tags = []string{}
	}

	tagsJSON, err := json.Marshal(pattern.Tags)
	if err != nil {
		return fmt.Errorf("marshaling tags: %w", err)
	}

	if pattern.RelatedPatterns == nil {
		pattern.RelatedPatterns = []string{}
	}

	relatedPatternsJSON, err := json.Marshal(pattern.RelatedPatterns)
	if err != nil {
		return fmt.Errorf("marshaling related_patterns: %w", err)
	}

	now := time.Now()
	query := `
		UPDATE patterns SET
			name = $2,
			description = $3,
			content = $4,
			tags = $5,
			entity_type = $6,
			language = $7,
			domain = $8,
			version = $9,
			related_patterns = $10,
			enrichment_status = 'pending',
			enrichment_error = NULL,
			enriched_at = NULL,
			updated_at = $11
		WHERE id = $1
	`

	result, err := r.db.Exec(ctx, query,
		pattern.ID,
		pattern.Name,
		pattern.Description,
		pattern.Content,
		tagsJSON,
		pattern.EntityType,
		pattern.Language,
		pattern.Domain,
		pattern.Version,
		relatedPatternsJSON,
		now,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == repository.PgErrCodeUniqueViolation {
			return ErrNameExists
		}
		return fmt.Errorf("updating pattern: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	// Update pattern struct to reflect the reset enrichment fields
	pattern.EnrichmentStatus = "pending"
	pattern.EnrichmentError = nil
	pattern.EnrichedAt = nil
	pattern.UpdatedAt = now
	return nil
}

// Delete removes a pattern by ID from the database.
func (r *pgxRepository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM patterns WHERE id = $1`

	result, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("deleting pattern: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// List retrieves patterns with filtering and pagination. Content is not
// selected: list responses only carry summary fields, and content is the
// largest column by far.
func (r *pgxRepository) List(ctx context.Context, filter Filter, opts repository.ListOptions) ([]*Pattern, error) {
	// Build query dynamically based on filter. Each condition takes the next
	// placeholder, numbered from the arguments collected so far.
	var whereConditions []string
	var args []any
	where := func(format string, arg any) {
		args = append(args, arg)
		whereConditions = append(whereConditions, fmt.Sprintf(format, len(args)))
	}

	if filter.EnrichmentStatus != "" {
		where("enrichment_status = $%d", filter.EnrichmentStatus)
	}

	if len(filter.Tags) > 0 {
		// Use ?& operator to check if tags contain ALL of the specified tags (AND logic)
		where("tags ?& $%d", filter.Tags)
	}

	if filter.SearchQuery != "" {
		// Full-text search on name and description
		where("to_tsvector('english', name || ' ' || coalesce(description, '')) @@ plainto_tsquery('english', $%d)", filter.SearchQuery)
	}

	if filter.Language != "" {
		where("language = $%d", filter.Language)
	}

	if filter.Domain != "" {
		where("domain = $%d", filter.Domain)
	}

	if filter.EntityType != "" {
		where("entity_type = $%d", filter.EntityType)
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT id, name, description, tags,
			   entity_type, language, domain, version, related_patterns,
			   enrichment_status, enrichment_error, enriched_at,
			   created_at, updated_at
		FROM patterns
		%s
		ORDER BY name ASC
	`, whereClause)

	// Add pagination
	if opts.Limit > 0 {
		args = append(args, opts.Limit)
		query += fmt.Sprintf(" LIMIT $%d", len(args))
	}
	if opts.Offset > 0 {
		args = append(args, opts.Offset)
		query += fmt.Sprintf(" OFFSET $%d", len(args))
	}

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing patterns: %w", err)
	}
	defer rows.Close()

	patterns := make([]*Pattern, 0)

	for rows.Next() {
		var p Pattern
		var tagsJSON []byte
		var relatedPatternsJSON []byte

		err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Description,
			&tagsJSON,
			&p.EntityType,
			&p.Language,
			&p.Domain,
			&p.Version,
			&relatedPatternsJSON,
			&p.EnrichmentStatus,
			&p.EnrichmentError,
			&p.EnrichedAt,
			&p.CreatedAt,
			&p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("listing patterns: scanning row: %w", err)
		}

		if err := json.Unmarshal(tagsJSON, &p.Tags); err != nil {
			return nil, fmt.Errorf("unmarshaling tags: %w", err)
		}

		if err := json.Unmarshal(relatedPatternsJSON, &p.RelatedPatterns); err != nil {
			return nil, fmt.Errorf("unmarshaling related_patterns: %w", err)
		}

		patterns = append(patterns, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing patterns: iterating rows: %w", err)
	}

	return patterns, nil
}

// WithTx returns a new Repository that executes all operations within db.
func (r *pgxRepository) WithTx(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}
