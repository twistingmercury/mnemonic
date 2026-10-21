package pattern_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/librarian-enricher/internal/repository/pattern"
)

func TestRepository_Get(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	desc := "a description"

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      *pattern.Pattern
		wantErr   error
	}{
		{
			name: "returns the pattern",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT id, name, description, content FROM patterns").
					WithArgs(id).
					WillReturnRows(pgxmock.NewRows([]string{"id", "name", "description", "content"}).
						AddRow(id, "error-handling", &desc, "# Error handling"))
			},
			want: &pattern.Pattern{ID: id, Name: "error-handling", Description: &desc, Content: "# Error handling"},
		},
		{
			name: "not found returns ErrNotFound",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT id, name, description, content FROM patterns").
					WithArgs(id).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: pattern.ErrNotFound,
		},
		{
			name: "context cancellation is propagated",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("SELECT id, name, description, content FROM patterns").
					WithArgs(id).
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

			got, err := pattern.NewRepository(mock).Get(context.Background(), id)

			if tt.wantErr != nil {
				assert.True(t, errors.Is(err, tt.wantErr), "got %v, want %v", err, tt.wantErr)
				assert.Nil(t, got)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_UpdateEnrichmentStatus(t *testing.T) {
	t.Parallel()

	errMsg := "embedding generation failed"

	tests := []struct {
		name      string
		patternID uuid.UUID
		status    string
		errMsg    *string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:      "update to enriched status",
			patternID: uuid.New(),
			status:    "enriched",
			errMsg:    nil,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(), // id
						"enriched",
						pgxmock.AnyArg(), // error message (nil *string)
						pgxmock.AnyArg(), // enriched_at (should be set)
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "update to failed status with error",
			patternID: uuid.New(),
			status:    "failed",
			errMsg:    &errMsg,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(), // id
						"failed",
						pgxmock.AnyArg(), // error message
						pgxmock.AnyArg(), // enriched_at (should be nil for failed)
						pgxmock.AnyArg(), // updated_at
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:      "pattern not found",
			patternID: uuid.New(),
			status:    "enriched",
			errMsg:    nil,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE patterns SET").
					WithArgs(
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
						pgxmock.AnyArg(),
					).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: pattern.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := pattern.NewRepository(mock)
			err = repo.UpdateEnrichmentStatus(context.Background(), tt.patternID, tt.status, tt.errMsg)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
