package main

import (
	"context"
	"log"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/postgres"

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
		config.Module(),
		postgres.Module(),
		runner.Module(),
		telegram.Module(),
	)
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
