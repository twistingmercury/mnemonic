package chunk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
	"github.com/twistingmercury/librarian-api/internal/repository"
)

// Repository defines data access operations for pattern chunks.
type Repository interface {
	// CreateBatch inserts multiple chunks of one pattern in a single statement.
	CreateBatch(ctx context.Context, chunks []*Chunk) error

	// ListSummariesByPatternID retrieves all chunks for a pattern, ordered by
	// chunk_index, without their content or embedding.
	ListSummariesByPatternID(ctx context.Context, patternID uuid.UUID) ([]*Chunk, error)

	// DeleteByPatternID removes all chunks belonging to a pattern.
	DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error

	// FindSimilar finds chunks similar to the given embedding vector.
	// Joins to patterns to return parent pattern metadata.
	FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error)

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

// WithTx returns a new Repository that executes all operations within db.
func (r *pgxRepository) WithTx(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// CreateBatch inserts chunks in one round trip. A single INSERT ... SELECT
// FROM unnest is atomic on its own, so it is safe on a pool or inside a
// caller-owned transaction alike. IDs are generated here when missing; the
// database fills enrichment_status, created_at and updated_at, which are not
// read back because callers only need the IDs.
func (r *pgxRepository) CreateBatch(ctx context.Context, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	ids := make([]uuid.UUID, len(chunks))
	titles := make([]string, len(chunks))
	indexes := make([]int32, len(chunks))
	contents := make([]string, len(chunks))
	for i, c := range chunks {
		if c.ID == uuid.Nil {
			c.ID = uuid.New()
		}
		ids[i] = c.ID
		titles[i] = c.SectionTitle
		indexes[i] = int32(c.ChunkIndex) // #nosec G115 -- chunk index is bounded by section count
		contents[i] = c.Content
	}

	// All chunks in a batch belong to one pattern (the only caller splits one
	// pattern's content), so pattern_id is a scalar.
	query := `
		INSERT INTO pattern_chunks (id, pattern_id, section_title, chunk_index, content)
		SELECT u.id, $1, u.section_title, u.chunk_index, u.content
		FROM unnest($2::uuid[], $3::text[], $4::int[], $5::text[])
		     AS u(id, section_title, chunk_index, content)
	`

	if _, err := r.db.Exec(ctx, query, chunks[0].PatternID, ids, titles, indexes, contents); err != nil {
		return fmt.Errorf("creating chunk batch: %w", err)
	}

	return nil
}

// ListSummariesByPatternID retrieves all chunks for a pattern, ordered by
// chunk_index. Content is left empty: listing callers only report per-chunk
// status, and chunk bodies can be large.
func (r *pgxRepository) ListSummariesByPatternID(ctx context.Context, patternID uuid.UUID) ([]*Chunk, error) {
	query := `
		SELECT id, pattern_id, section_title, chunk_index,
		       enrichment_status, enrichment_error, enriched_at,
		       created_at, updated_at
		FROM pattern_chunks
		WHERE pattern_id = $1
		ORDER BY chunk_index ASC
	`

	rows, err := r.db.Query(ctx, query, patternID)
	if err != nil {
		return nil, fmt.Errorf("listing chunks by pattern: %w", err)
	}
	defer rows.Close()

	chunks := make([]*Chunk, 0)

	for rows.Next() {
		var c Chunk
		err := rows.Scan(
			&c.ID,
			&c.PatternID,
			&c.SectionTitle,
			&c.ChunkIndex,
			&c.EnrichmentStatus,
			&c.EnrichmentError,
			&c.EnrichedAt,
			&c.CreatedAt,
			&c.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("listing chunks by pattern: scanning row: %w", err)
		}
		chunks = append(chunks, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing chunks by pattern: iterating rows: %w", err)
	}

	return chunks, nil
}

// DeleteByPatternID removes all chunks belonging to a pattern.
func (r *pgxRepository) DeleteByPatternID(ctx context.Context, patternID uuid.UUID) error {
	query := `DELETE FROM pattern_chunks WHERE pattern_id = $1`

	_, err := r.db.Exec(ctx, query, patternID)
	if err != nil {
		return fmt.Errorf("deleting chunks by pattern: %w", err)
	}

	return nil
}

// FindSimilar finds chunks similar to the given embedding vector.
// Joins to patterns to return parent pattern metadata.
func (r *pgxRepository) FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error) {
	vec := pgvector.NewVector(embedding)

	// Build query dynamically to support the optional Tags filter.
	// Fixed parameters: $1 = embedding vector, $2 = min similarity, $3 = language, $4 = domain, $5 = limit.
	query := `
		SELECT pc.id, pc.pattern_id, p.name, p.entity_type, p.language, p.domain, p.tags,
		       pc.section_title, pc.chunk_index, pc.content,
		       1 - (pc.embedding <=> $1) AS similarity
		FROM pattern_chunks pc
		JOIN patterns p ON p.id = pc.pattern_id
		WHERE pc.embedding IS NOT NULL
		  AND pc.enrichment_status = 'enriched'
		  AND 1 - (pc.embedding <=> $1) >= $2
		  AND ($3::text = '' OR p.language = $3)
		  AND ($4::text = '' OR p.domain = $4)
	`

	args := []any{vec, opts.MinSimilarity, opts.Language, opts.Domain}
	nextParam := 5

	if len(opts.Tags) > 0 {
		tagsJSON, err := json.Marshal(opts.Tags)
		if err != nil {
			return nil, fmt.Errorf("finding similar chunks: marshaling tags filter: %w", err)
		}
		query += fmt.Sprintf(" AND p.tags @> $%d::jsonb", nextParam)
		args = append(args, string(tagsJSON))
		nextParam++
	}

	query += fmt.Sprintf(" ORDER BY pc.embedding <=> $1 LIMIT $%d", nextParam)
	args = append(args, opts.MaxResults)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("finding similar chunks: %w", err)
	}
	defer rows.Close()

	matches := make([]*Match, 0)

	for rows.Next() {
		var m Match
		var tagsJSON []byte

		err := rows.Scan(
			&m.ChunkID,
			&m.PatternID,
			&m.PatternName,
			&m.EntityType,
			&m.Language,
			&m.Domain,
			&tagsJSON,
			&m.SectionTitle,
			&m.ChunkIndex,
			&m.Content,
			&m.Similarity,
		)
		if err != nil {
			return nil, fmt.Errorf("finding similar chunks: scanning row: %w", err)
		}

		if err := json.Unmarshal(tagsJSON, &m.Tags); err != nil {
			return nil, fmt.Errorf("finding similar chunks: unmarshaling tags: %w", err)
		}

		matches = append(matches, &m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finding similar chunks: iterating rows: %w", err)
	}

	return matches, nil
}
