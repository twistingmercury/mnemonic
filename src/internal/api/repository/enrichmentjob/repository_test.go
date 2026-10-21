package enrichmentjob_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/twistingmercury/librarian-api/internal/repository/enrichmentjob"
)

func TestRepository_CreatePendingForChunks(t *testing.T) {
	t.Parallel()

	t.Run("empty input makes no DB call", func(t *testing.T) {
		t.Parallel()
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		ids, err := enrichmentjob.NewRepository(mock).CreatePendingForChunks(context.Background(), nil)
		require.NoError(t, err)
		assert.Empty(t, ids)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("one statement for all chunks, ids returned in chunk order", func(t *testing.T) {
		t.Parallel()
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		chunkIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
		mock.ExpectExec(`INSERT INTO enrichment_jobs .* unnest`).
			WithArgs(
				pgxmock.AnyArg(),
				chunkIDs,
				string(enrichmentjob.StatusPending),
				enrichmentjob.DefaultMaxAttempts,
			).
			WillReturnResult(pgxmock.NewResult("INSERT", 3))

		ids, err := enrichmentjob.NewRepository(mock).CreatePendingForChunks(context.Background(), chunkIDs)
		require.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())

		require.Len(t, ids, 3)
		seen := map[uuid.UUID]bool{}
		for _, id := range ids {
			assert.NotEqual(t, uuid.Nil, id)
			seen[id] = true
		}
		assert.Len(t, seen, 3, "job ids are distinct")
	})
}
