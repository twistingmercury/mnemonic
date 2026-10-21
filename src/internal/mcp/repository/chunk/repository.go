package chunk

import (
	"context"
	"fmt"

	"github.com/pgvector/pgvector-go"
	"github.com/twistingmercury/librarian-mcp/internal/repository"
)

// Repository defines the chunk reads the MCP tools need.
type Repository interface {
	// FindSimilar finds chunks similar to the given embedding vector.
	// Joins to patterns to return parent pattern metadata.
	FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error)
}

// pgxRepository is a PostgreSQL implementation of Repository using pgx.
type pgxRepository struct {
	db repository.DBTX
}

// NewRepository creates a new PostgreSQL-backed Repository.
func NewRepository(db repository.DBTX) Repository {
	return &pgxRepository{db: db}
}

// FindSimilar finds chunks similar to the given embedding vector.
// Joins to patterns to return parent pattern metadata.
func (r *pgxRepository) FindSimilar(ctx context.Context, embedding []float32, opts SimilarityOptions) ([]*Match, error) {
	// $5 is the conjunctive tag filter; a nil slice is sent as SQL NULL.
	const query = `
		SELECT pc.pattern_id, p.name, p.tags, pc.section_title, pc.content,
		       1 - (pc.embedding <=> $1) AS similarity
		FROM pattern_chunks pc
		JOIN patterns p ON p.id = pc.pattern_id
		WHERE pc.embedding IS NOT NULL
		  AND pc.enrichment_status = 'enriched'
		  AND 1 - (pc.embedding <=> $1) >= $2
		  AND ($3::text = '' OR p.language = $3)
		  AND ($4::text = '' OR p.domain = $4)
		  AND ($5::jsonb IS NULL OR p.tags @> $5::jsonb)
		ORDER BY pc.embedding <=> $1
		LIMIT $6
	`

	rows, err := r.db.Query(ctx, query, pgvector.NewVector(embedding), opts.MinSimilarity, opts.Language, opts.Domain, opts.Tags, opts.MaxResults)
	if err != nil {
		return nil, fmt.Errorf("finding similar chunks: %w", err)
	}
	defer rows.Close()

	matches := make([]*Match, 0, opts.MaxResults)

	for rows.Next() {
		var m Match
		if err := rows.Scan(&m.PatternID, &m.PatternName, &m.Tags, &m.SectionTitle, &m.Content, &m.Similarity); err != nil {
			return nil, fmt.Errorf("finding similar chunks: scanning row: %w", err)
		}
		matches = append(matches, &m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("finding similar chunks: iterating rows: %w", err)
	}

	return matches, nil
}
