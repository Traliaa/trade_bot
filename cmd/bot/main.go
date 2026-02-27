package main

import (
	"context"
	"log"
	"trade_bot/internal/modules/bootstrap"
	"trade_bot/internal/modules/config"

	"trade_bot/internal/modules/okx_websocket"
	"trade_bot/internal/modules/postgres"
	"trade_bot/internal/modules/repository"
	"trade_bot/internal/modules/strategy"
	telegram "trade_bot/internal/modules/telegram_bot"
	"trade_bot/internal/modules/telegram_public"
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

		postgres.Module(),
		repository.Module(),
		config.Module(),

		okx_websocket.Module(),
		strategy.Module(),
		bootstrap.Module(),
		runner.Module(),
		telegram.Module(),
		telegram_public.Module(),
		//api.Module(),
		//httpserver.Module(),

	)
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
