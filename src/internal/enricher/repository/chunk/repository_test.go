package chunk_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/librarian-enricher/internal/repository/chunk"
)

func newMock(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, mock.ExpectationsWereMet())
		mock.Close()
	})
	return mock
}

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	id, patternID := uuid.New(), uuid.New()
	cols := []string{"id", "pattern_id", "section_title", "content", "name", "tags"}

	t.Run("returns the chunk with its pattern's name and tags", func(t *testing.T) {
		t.Parallel()
		mock := newMock(t)
		mock.ExpectQuery("FROM pattern_chunks pc\\s+JOIN patterns p").
			WithArgs(id).
			WillReturnRows(pgxmock.NewRows(cols).
				AddRow(id, patternID, "Overview", "body", "error-handling", []byte(`["go","errors"]`)))

		got, err := chunk.NewRepository(mock).Get(context.Background(), id)
		require.NoError(t, err)
		assert.Equal(t, &chunk.Chunk{
			ID: id, PatternID: patternID, SectionTitle: "Overview", Content: "body",
			PatternName: "error-handling", PatternTags: []string{"go", "errors"},
		}, got)
	})

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		t.Parallel()
		mock := newMock(t)
		mock.ExpectQuery("FROM pattern_chunks").WithArgs(id).WillReturnError(pgx.ErrNoRows)

		_, err := chunk.NewRepository(mock).Get(context.Background(), id)
		assert.ErrorIs(t, err, chunk.ErrNotFound)
	})

	t.Run("bad tags JSON is an error", func(t *testing.T) {
		t.Parallel()
		mock := newMock(t)
		mock.ExpectQuery("FROM pattern_chunks").
			WithArgs(id).
			WillReturnRows(pgxmock.NewRows(cols).AddRow(id, patternID, "Overview", "body", "p", []byte(`{`)))

		_, err := chunk.NewRepository(mock).Get(context.Background(), id)
		assert.ErrorContains(t, err, "unmarshaling pattern tags")
	})
}

func TestRepository_MarkEnriched(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	embedding := []float32{0.1, 0.2, 0.3}

	tests := []struct {
		name    string
		rows    int64
		dbErr   error
		wantErr error
	}{
		{name: "stores embedding and status", rows: 1},
		{name: "not found returns ErrNotFound", rows: 0, wantErr: chunk.ErrNotFound},
		{name: "database error is propagated", dbErr: errors.New("boom"), wantErr: errors.New("boom")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := newMock(t)
			exp := mock.ExpectExec("UPDATE pattern_chunks SET\\s+embedding = \\$2,\\s+enrichment_status = 'enriched'").
				WithArgs(id, pgvector.NewVector(embedding))
			if tt.dbErr != nil {
				exp.WillReturnError(tt.dbErr)
			} else {
				exp.WillReturnResult(pgxmock.NewResult("UPDATE", tt.rows))
			}

			err := chunk.NewRepository(mock).MarkEnriched(context.Background(), id, embedding)
			switch {
			case tt.wantErr == nil:
				assert.NoError(t, err)
			case errors.Is(tt.wantErr, chunk.ErrNotFound):
				assert.ErrorIs(t, err, chunk.ErrNotFound)
			default:
				assert.ErrorContains(t, err, tt.wantErr.Error())
			}
		})
	}
}

func TestRepository_MarkFailed(t *testing.T) {
	t.Parallel()

	id := uuid.New()

	t.Run("records the error", func(t *testing.T) {
		t.Parallel()
		mock := newMock(t)
		mock.ExpectExec("enrichment_status = 'failed'").
			WithArgs(id, "embed chunk: boom").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		assert.NoError(t, chunk.NewRepository(mock).MarkFailed(context.Background(), id, "embed chunk: boom"))
	})

	t.Run("not found returns ErrNotFound", func(t *testing.T) {
		t.Parallel()
		mock := newMock(t)
		mock.ExpectExec("enrichment_status = 'failed'").
			WithArgs(id, "x").
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		assert.ErrorIs(t, chunk.NewRepository(mock).MarkFailed(context.Background(), id, "x"), chunk.ErrNotFound)
	})
}

func TestRepository_PatternProgress(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()

	tests := []struct {
		name        string
		anyFailed   bool
		allEnriched bool
	}{
		{name: "all enriched", allEnriched: true},
		{name: "some pending", anyFailed: false, allEnriched: false},
		{name: "one failed", anyFailed: true, allEnriched: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := newMock(t)
			mock.ExpectQuery("FROM pattern_chunks\\s+WHERE pattern_id = \\$1").
				WithArgs(patternID).
				WillReturnRows(pgxmock.NewRows([]string{"any_failed", "all_enriched"}).AddRow(tt.anyFailed, tt.allEnriched))

			got, err := chunk.NewRepository(mock).PatternProgress(context.Background(), patternID)
			require.NoError(t, err)
			assert.Equal(t, chunk.Progress{AnyFailed: tt.anyFailed, AllEnriched: tt.allEnriched}, got)
		})
	}

	t.Run("database error is wrapped", func(t *testing.T) {
		t.Parallel()
		mock := newMock(t)
		mock.ExpectQuery("FROM pattern_chunks").WithArgs(patternID).WillReturnError(errors.New("boom"))

		_, err := chunk.NewRepository(mock).PatternProgress(context.Background(), patternID)
		assert.ErrorContains(t, err, "checking chunk progress for pattern: boom")
	})
}
