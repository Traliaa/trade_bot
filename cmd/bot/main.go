package main

import (
	"context"
	"log"
	"trade_bot/internal/modules/bootstrap"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/health"
	"trade_bot/internal/modules/okx_websocket"
	"trade_bot/internal/modules/postgres"
	"trade_bot/internal/modules/strategy"
	telegram "trade_bot/internal/modules/telegram_bot"

	"trade_bot/internal/runner"

	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		fx.Provide(
			func() context.Context {
				return context.Background()
			},
		),
		health.Module(),
		config.Module(),
		postgres.Module(),
		okx_websocket.Module(),
		strategy.Module(),
		bootstrap.Module(),
		runner.Module(),
		telegram.Module(),
	)
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
