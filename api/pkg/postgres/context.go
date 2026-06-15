package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey string

const txContextKey = contextKey("tx")

func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txContextKey, tx)
}

func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	if value := ctx.Value(txContextKey); value != nil {
		tx, ok := value.(pgx.Tx)
		return tx, ok
	}
	return nil, false
}

type Transactioner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

func BeginTxFunc(ctx context.Context, db Transactioner, txOptions pgx.TxOptions, fn func(ctx context.Context) error) error {
	// Already inside a transaction, so join it. The outermost caller owns
	// commit/rollback; inner errors propagate up and trigger rollback there.
	if _, inTx := TxFromContext(ctx); inTx {
		return fn(ctx)
	}

	return pgx.BeginTxFunc(ctx, db, txOptions, func(tx pgx.Tx) error {
		return fn(WithTx(ctx, tx))
	})
}

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func QueryOne[T any](ctx context.Context, pool *pgxpool.Pool, query string, args []any, mapper pgx.RowToFunc[T]) (T, error) {
	var zero T

	var db Querier = pool
	if tx, inTx := TxFromContext(ctx); inTx {
		db = tx
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return zero, fmt.Errorf("querying: %w", err)
	}

	mapped, err := pgx.CollectExactlyOneRow(rows, mapper)
	if err != nil {
		return zero, fmt.Errorf("collecting row: %w", err)
	}
	return mapped, nil
}

func QueryMany[T any](ctx context.Context, pool *pgxpool.Pool, query string, args []any, mapper pgx.RowToFunc[T]) ([]T, error) {
	var db Querier = pool
	if tx, inTx := TxFromContext(ctx); inTx {
		db = tx
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying: %w", err)
	}

	mapped, err := pgx.CollectRows(rows, mapper)
	if err != nil {
		return nil, fmt.Errorf("collecting rows: %w", err)
	}
	return mapped, nil
}

type Execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func Exec(ctx context.Context, pool *pgxpool.Pool, query string, args []any) (pgconn.CommandTag, error) {
	var db Execer = pool
	if tx, inTx := TxFromContext(ctx); inTx {
		db = tx
	}
	return db.Exec(ctx, query, args...)
}

func QueryRow(ctx context.Context, pool *pgxpool.Pool, query string, args []any) pgx.Row {
	var db Querier = pool
	if tx, inTx := TxFromContext(ctx); inTx {
		db = tx
	}
	return db.QueryRow(ctx, query, args...)
}
