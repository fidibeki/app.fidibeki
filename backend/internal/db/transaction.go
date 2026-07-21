package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Transaction interface {
	Querier
	Commit(context.Context) error
	Rollback(context.Context) error
}

type BeginFunc func(context.Context) (Transaction, error)

func BeginFromPool(pool interface {
	Begin(context.Context) (pgx.Tx, error)
}) BeginFunc {
	return func(ctx context.Context) (Transaction, error) {
		return pool.Begin(ctx)
	}
}
