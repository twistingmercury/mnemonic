package pattern_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twistingmercury/librarian/internal/mcp/repository/pattern"
)

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	now := time.Now()
	patternID := uuid.New()
	tags := []string{"tag1", "tag2"}
	errMsg := "boom"
	columns := []string{"id", "name", "content", "tags", "enrichment_status", "enrichment_error", "enriched_at"}

	tests := []struct {
		name        string
		setupMock   func(mock pgxmock.PgxPoolIface)
		wantPattern *pattern.Pattern
		wantErr     error
	}{
		{
			name: "enriched pattern",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT id, name, content, tags, .* FROM patterns").
					WithArgs(patternID).
					WillReturnRows(pgxmock.NewRows(columns).
						AddRow(patternID, "test-pattern", "Test content", tags, "enriched", nil, &now))
			},
			wantPattern: &pattern.Pattern{
				ID:               patternID,
				Name:             "test-pattern",
				Content:          "Test content",
				Tags:             []string{"tag1", "tag2"},
				EnrichmentStatus: "enriched",
				EnrichedAt:       &now,
			},
		},
		{
			name: "failed pattern",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs(patternID).
					WillReturnRows(pgxmock.NewRows(columns).
						AddRow(patternID, "test-pattern", "Test content", tags, "failed", &errMsg, nil))
			},
			wantPattern: &pattern.Pattern{
				ID:               patternID,
				Name:             "test-pattern",
				Content:          "Test content",
				Tags:             []string{"tag1", "tag2"},
				EnrichmentStatus: "failed",
				EnrichmentError:  &errMsg,
			},
		},
		{
			name: "not found returns ErrNotFound",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs(patternID).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: pattern.ErrNotFound,
		},
		{
			name: "context cancellation is propagated",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT .* FROM patterns").
					WithArgs(patternID).
					WillReturnError(context.Canceled)
			},
			wantErr: context.Canceled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()
			tt.setupMock(mock)

			p, err := pattern.NewRepository(mock).Get(context.Background(), patternID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, p)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPattern, p)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_GetName(t *testing.T) {
	t.Parallel()

	patternID := uuid.New()
	dbErr := errors.New("connection failed")

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantName  string
		wantErr   error
	}{
		{
			name: "found",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT name FROM patterns").
					WithArgs(patternID).
					WillReturnRows(pgxmock.NewRows([]string{"name"}).AddRow("go-errors"))
			},
			wantName: "go-errors",
		},
		{
			name: "not found returns ErrNotFound",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT name FROM patterns").
					WithArgs(patternID).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: pattern.ErrNotFound,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT name FROM patterns").
					WithArgs(patternID).
					WillReturnError(dbErr)
			},
			wantErr: dbErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()
			tt.setupMock(mock)

			name, err := pattern.NewRepository(mock).GetName(context.Background(), patternID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantName, name)
			}
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestGetByIDs(t *testing.T) {
	t.Parallel()

	id1 := uuid.New()
	id2 := uuid.New()
	tags := []string{"test", "example"}
	columns := []string{"id", "name", "tags", "language", "domain"}

	newRow := func(id uuid.UUID, name string) []any {
		return []any{id, name, tags, "go", "backend"}
	}

	t.Run("success multiple IDs all found", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		rows := pgxmock.NewRows(columns).
			AddRow(newRow(id1, "pattern-one")...).
			AddRow(newRow(id2, "pattern-two")...)

		mock.ExpectQuery("SELECT id, name, tags, language, domain FROM patterns").
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(rows)

		result, err := pattern.NewRepository(mock).GetByIDs(context.Background(), []uuid.UUID{id1, id2})

		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, &pattern.Pattern{
			ID: id1, Name: "pattern-one", Tags: []string{"test", "example"}, Language: "go", Domain: "backend",
		}, result[0])
		assert.Equal(t, id2, result[1].ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty slice returns empty result without DB call", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// No expectations set — any DB call would cause a failure.
		result, err := pattern.NewRepository(mock).GetByIDs(context.Background(), []uuid.UUID{})

		require.NoError(t, err)
		assert.Empty(t, result)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("partial match returns only found patterns", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("SELECT .* FROM patterns").
			WithArgs(pgxmock.AnyArg()).
			WillReturnRows(pgxmock.NewRows(columns).AddRow(newRow(id1, "pattern-one")...))

		result, err := pattern.NewRepository(mock).GetByIDs(context.Background(), []uuid.UUID{id1, uuid.New()})

		require.NoError(t, err)
		require.Len(t, result, 1)
		assert.Equal(t, id1, result[0].ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("query error returns wrapped error", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		dbErr := errors.New("connection refused")
		mock.ExpectQuery("SELECT .* FROM patterns").
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(dbErr)

		result, err := pattern.NewRepository(mock).GetByIDs(context.Background(), []uuid.UUID{id1})

		assert.Nil(t, result)
		assert.ErrorContains(t, err, "getting patterns by IDs")
		assert.ErrorIs(t, err, dbErr)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
