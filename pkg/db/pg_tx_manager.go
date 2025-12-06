package db

import (
	"context"
	"fmt"
	"trade_bot/pkg/logger"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const dsnFormat = "postgres://%s:%s@%s:%d/%s?sslmode=disable"

type PoolConfig struct {
	DSN string
}

type PgTxManager struct {
	poolMaster *pgxpool.Pool
}

func NewPgTxManager(poolMaster *pgxpool.Pool) *PgTxManager {
	return &PgTxManager{
		poolMaster: poolMaster,
	}
}

func (m *PgTxManager) Close() {
	m.poolMaster.Close()
}

func NewPool(ctx context.Context, conf PoolConfig) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, conf.DSN)
}

func (m *PgTxManager) RunMaster(ctx context.Context, fn func(ctxTx context.Context, tx pgx.Tx) error) error {
	options := pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	}
	// то что запрос нужно выполнить на мастере еще не означает что это нужно выполнить в транзакции, может требоваться
	// просто согласованное чтение, например.
	return m.inTx(ctx, m.poolMaster, options, fn)
}

func (m *PgTxManager) Conn() Transaction {
	return m.poolMaster
}

func (m *PgTxManager) inTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	options pgx.TxOptions,
	f func(ctxTx context.Context, tx pgx.Tx) error,
) error {
	tx, err := pool.BeginTx(ctx, options)
	if err != nil {
		return fmt.Errorf("failed to begin tx, err: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			logger.Info("%v", p)
			_ = tx.Rollback(ctx)
			panic(p) // fallthrough panic after rollback on caught panic
		} else if err != nil {
			_ = tx.Rollback(ctx) // if error during computations
		} else {
			err = tx.Commit(ctx) // all good
		}
	}()

	err = f(ctx, tx)
	if err != nil {
		return fmt.Errorf("failed to run fn, err: %w", err)
	}

	return nil
}
