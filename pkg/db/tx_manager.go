package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type TxManager interface {
	RunMaster(ctx context.Context, fn func(ctxTx context.Context, tx Transaction) error) error
	RunReplica(ctx context.Context, fn func(ctxTx context.Context, tx Transaction) error) error
	RunRepeatableRead(ctx context.Context, fn func(ctxTx context.Context, tx Transaction) error) error
}

type Transaction interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row
}
