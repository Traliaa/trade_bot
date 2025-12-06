package main

import (
	"context"
	"log"
	"trade_bot/internal/modules/config"

	telegram "trade_bot/internal/modules/telegram_bot"

	"trade_bot/internal/runner"

	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		config.Module(),
		runner.Module(),
		telegram.Module(),
	)
	if err := app.Start(context.Background()); err != nil {
		log.Fatal(err)
	}
}
