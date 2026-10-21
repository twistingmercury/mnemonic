package chunk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/librarian-mcp/internal/repository/chunk"
)

// testEmbedding returns a sample embedding vector for testing.
func testEmbedding() []float32 {
	emb := make([]float32, 1536)
	for i := range emb {
		emb[i] = float32(i) * 0.001
	}
	return emb
}

var matchColumns = []string{"pattern_id", "pattern_name", "tags", "section_title", "content", "similarity"}

func TestRepository_FindSimilar(t *testing.T) {
	t.Parallel()

	emb := testEmbedding()
	opts := chunk.SimilarityOptions{MinSimilarity: 0.7, MaxResults: 5}
	patternID := uuid.New()

	tests := []struct {
		name      string
		opts      chunk.SimilarityOptions
		setupMock func(mock pgxmock.PgxPoolIface)
		want      []*chunk.Match
		wantErr   bool
	}{
		{
			name: "returns matching chunks with pattern metadata",
			opts: opts,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows(matchColumns).
					AddRow(patternID, "go-error-handling", []string{"go", "patterns"}, "Overview", "content here", 0.85)
				mock.ExpectQuery("SELECT pc.pattern_id, p.name, p.tags, pc.section_title, pc.content,.* FROM pattern_chunks pc").
					WithArgs(pgxmock.AnyArg(), opts.MinSimilarity, opts.Language, opts.Domain, opts.Tags, opts.MaxResults).
					WillReturnRows(rows)
			},
			want: []*chunk.Match{{
				PatternID:    patternID,
				PatternName:  "go-error-handling",
				Tags:         []string{"go", "patterns"},
				SectionTitle: "Overview",
				Content:      "content here",
				Similarity:   0.85,
			}},
		},
		{
			name: "no results returns empty slice",
			opts: opts,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM pattern_chunks pc").
					WithArgs(pgxmock.AnyArg(), opts.MinSimilarity, opts.Language, opts.Domain, opts.Tags, opts.MaxResults).
					WillReturnRows(pgxmock.NewRows(matchColumns))
			},
			want: []*chunk.Match{},
		},
		{
			name: "with language, domain and tag filters",
			opts: chunk.SimilarityOptions{
				MinSimilarity: 0.5,
				MaxResults:    10,
				Language:      "go",
				Domain:        "engineering",
				Tags:          []string{"go", "errors"},
			},
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery(`AND \(\$5::jsonb IS NULL OR p.tags @> \$5::jsonb\)\s+ORDER BY pc.embedding <=> \$1\s+LIMIT \$6`).
					WithArgs(pgxmock.AnyArg(), 0.5, "go", "engineering", []string{"go", "errors"}, 10).
					WillReturnRows(pgxmock.NewRows(matchColumns))
			},
			want: []*chunk.Match{},
		},
		{
			name: "database error is propagated",
			opts: opts,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM pattern_chunks pc").
					WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnError(errors.New("connection failed"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			matches, err := chunk.NewRepository(mock).FindSimilar(context.Background(), emb, tt.opts)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, matches)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
