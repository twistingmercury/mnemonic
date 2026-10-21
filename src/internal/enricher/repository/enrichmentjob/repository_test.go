package enrichmentjob_test

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

	"github.com/twistingmercury/mnemonic/internal/enricher/repository/enrichmentjob"
)

func TestRepository_Start(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	chunkID := uuid.New()

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		want      *enrichmentjob.Job
		wantErr   error
	}{
		{
			name: "marks processing and returns the job",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("WHERE id = \\$1 AND status = \\$3 AND scheduled_for <= now\\(\\)").
					WithArgs(jobID, "processing", "pending").
					WillReturnRows(pgxmock.NewRows([]string{"id", "pattern_id", "chunk_id"}).
						AddRow(jobID, (*uuid.UUID)(nil), &chunkID))
			},
			want: &enrichmentjob.Job{ID: jobID, ChunkID: &chunkID},
		},
		{
			name: "missing, started or not due returns ErrUnavailable",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing", "pending").
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: enrichmentjob.ErrUnavailable,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectQuery("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "processing", "pending").
					WillReturnError(errors.New("connection refused"))
			},
			wantErr: errors.New("start job: connection refused"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			got, err := enrichmentjob.NewRepository(mock).Start(context.Background(), jobID)

			switch {
			case errors.Is(tt.wantErr, enrichmentjob.ErrUnavailable):
				assert.ErrorIs(t, err, tt.wantErr)
			case tt.wantErr != nil:
				assert.EqualError(t, err, tt.wantErr.Error())
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_ClaimDue(t *testing.T) {
	t.Parallel()

	chunkA, chunkB := uuid.New(), uuid.New()
	jobA, jobB := uuid.New(), uuid.New()

	t.Run("claims due jobs with SKIP LOCKED", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
			WithArgs("processing", "pending", 2*time.Minute, 4).
			WillReturnRows(pgxmock.NewRows([]string{"id", "pattern_id", "chunk_id"}).
				AddRow(jobA, (*uuid.UUID)(nil), &chunkA).
				AddRow(jobB, (*uuid.UUID)(nil), &chunkB))

		got, err := enrichmentjob.NewRepository(mock).ClaimDue(context.Background(), 2*time.Minute, 4)

		require.NoError(t, err)
		assert.Equal(t, []*enrichmentjob.Job{{ID: jobA, ChunkID: &chunkA}, {ID: jobB, ChunkID: &chunkB}}, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no due jobs returns an empty result", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
			WithArgs("processing", "pending", 2*time.Minute, 4).
			WillReturnRows(pgxmock.NewRows([]string{"id", "pattern_id", "chunk_id"}))

		got, err := enrichmentjob.NewRepository(mock).ClaimDue(context.Background(), 2*time.Minute, 4)

		require.NoError(t, err)
		assert.Empty(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error is wrapped", func(t *testing.T) {
		t.Parallel()

		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		mock.ExpectQuery("FOR UPDATE SKIP LOCKED").
			WithArgs("processing", "pending", 2*time.Minute, 4).
			WillReturnError(errors.New("boom"))

		_, err = enrichmentjob.NewRepository(mock).ClaimDue(context.Background(), 2*time.Minute, 4)

		assert.ErrorContains(t, err, "claim due jobs: boom")
	})
}

func TestRepository_MarkCompleted(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()

	tests := []struct {
		name      string
		jobID     uuid.UUID
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
	}{
		{
			name:  "successful update",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "completed").
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))
			},
			wantErr: nil,
		},
		{
			name:  "not found returns ErrNotFound",
			jobID: jobID,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs(jobID, "completed").
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantErr: enrichmentjob.ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			err = repo.MarkCompleted(context.Background(), tt.jobID)

			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_MarkFailed_WithRetry(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	retryDelay := 5 * time.Minute

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	// Single atomic UPDATE with CASE expression
	mock.ExpectExec("UPDATE enrichment_jobs SET").
		WithArgs(
			jobID,
			"pending",
			"failed",
			"test error", // last_error
			retryDelay,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	repo := enrichmentjob.NewRepository(mock)
	err = repo.MarkFailed(context.Background(), jobID, errors.New("test error"), retryDelay)

	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_MarkFailed_NotFound(t *testing.T) {
	t.Parallel()

	jobID := uuid.New()
	retryDelay := 5 * time.Minute

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec("UPDATE enrichment_jobs SET").
		WithArgs(
			jobID,
			"pending",
			"failed",
			pgxmock.AnyArg(),
			retryDelay,
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	repo := enrichmentjob.NewRepository(mock)
	err = repo.MarkFailed(context.Background(), jobID, errors.New("error"), retryDelay)

	assert.ErrorIs(t, err, enrichmentjob.ErrNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepository_ReclaimStale(t *testing.T) {
	t.Parallel()

	timeout := 30 * time.Minute

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   error
	}{
		{
			name: "reclaims stale jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs("pending", "failed", "processing", 30*time.Minute).
					WillReturnResult(pgxmock.NewResult("UPDATE", 5))
			},
			wantCount: 5,
		},
		{
			name: "no stale jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs("pending", "failed", "processing", 30*time.Minute).
					WillReturnResult(pgxmock.NewResult("UPDATE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("UPDATE enrichment_jobs SET").
					WithArgs("pending", "failed", "processing", 30*time.Minute).
					WillReturnError(errors.New("connection reset"))
			},
			wantCount: 0,
			wantErr:   errors.New("reclaim stale jobs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			count, err := repo.ReclaimStale(context.Background(), timeout)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "reclaim stale jobs")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_DeleteCompleted(t *testing.T) {
	t.Parallel()

	retention := 7 * 24 * time.Hour // 7 days

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   error
	}{
		{
			name: "deletes old completed jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("completed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 10))
			},
			wantCount: 10,
		},
		{
			name: "no jobs to delete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("completed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("completed", pgxmock.AnyArg()).
					WillReturnError(errors.New("connection reset"))
			},
			wantCount: 0,
			wantErr:   errors.New("delete completed jobs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			count, err := repo.DeleteCompleted(context.Background(), retention)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "delete completed jobs")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestRepository_DeleteFailed(t *testing.T) {
	t.Parallel()

	retention := 30 * 24 * time.Hour // 30 days

	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantCount int64
		wantErr   error
	}{
		{
			name: "deletes old failed jobs",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("failed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 3))
			},
			wantCount: 3,
		},
		{
			name: "no jobs to delete",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("failed", pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))
			},
			wantCount: 0,
		},
		{
			name: "database error is wrapped",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectExec("DELETE FROM enrichment_jobs WHERE status").
					WithArgs("failed", pgxmock.AnyArg()).
					WillReturnError(errors.New("connection reset"))
			},
			wantCount: 0,
			wantErr:   errors.New("delete failed jobs"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, err := pgxmock.NewPool()
			require.NoError(t, err)
			defer mock.Close()

			tt.setupMock(mock)

			repo := enrichmentjob.NewRepository(mock)
			count, err := repo.DeleteFailed(context.Background(), retention)

			if tt.wantErr != nil {
				assert.Error(t, err)
				assert.ErrorContains(t, err, "delete failed jobs")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantCount, count)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
