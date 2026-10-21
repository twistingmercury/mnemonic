package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// DBTX is the subset of *pgxpool.Pool the read-only repositories use. pgxmock
// satisfies it too, so repositories can be unit tested without a database.
type DBTX interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
