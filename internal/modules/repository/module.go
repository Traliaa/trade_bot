package repository

import (
	"trade_bot/internal/modules/repository/pg"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("repository",
		fx.Provide(
			pg.NewUser,
		),
	)
}
