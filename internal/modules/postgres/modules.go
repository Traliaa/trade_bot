package postgres

import (
	"context"
	"fmt"
	"trade_bot/internal/modules/config"
	"trade_bot/pkg/db"

	"go.uber.org/fx"
)

// ProvideAppConfig регистрируем как fx-провайдер.
func Module() fx.Option {
	return fx.Module("postgres",
		fx.Provide(
			func(ctx context.Context, cfg *config.Config) (*db.PgTxManager, error) {
				poolMaster, err := db.NewPool(ctx, db.PoolConfig{
					DSN: cfg.DB,
				})
				if err != nil {
					return nil, fmt.Errorf("failed to create poolMaster: %w", err)
				}

				err = poolMaster.Ping(ctx)
				if err != nil {
					return nil, err
				}

				return db.NewPgTxManager(poolMaster), nil
			},
		),
	)
}
