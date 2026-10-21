package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ListOptions defines pagination parameters for list operations.
type ListOptions struct {
	// Limit specifies the maximum number of items to return.
	// A value of 0 means no limit.
	Limit int

	// Offset specifies the number of items to skip before returning results.
	Offset int
}

// DBTX is an interface that both *pgxpool.Pool and pgxmock can satisfy.
// It defines the database operations required by the repository.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxBeginner allows starting database transactions.
// Use this interface when service layer needs to coordinate multiple repository operations.
// Note: pgx.Tx satisfies DBTX, so repositories can accept either a pool or a transaction.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}
