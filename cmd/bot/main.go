package main

import (
	"context"
	"time"
	"trade_bot/internal/modules/api"
	"trade_bot/internal/modules/bootstrap"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/httpserver"
	"trade_bot/internal/modules/okx_websocket"
	"trade_bot/internal/modules/postgres"
	"trade_bot/internal/modules/repository"
	"trade_bot/internal/modules/runner_old"
	"trade_bot/internal/modules/strategy"
	telegram "trade_bot/internal/modules/telegram_bot"
	"trade_bot/internal/modules/telegram_public"

	"trade_bot/pkg/logger"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func main() {
	var processID = time.Now().UnixNano()

	// в NewWarmuper / Warmup / SetWarmupDone / AutoTuneNow:
	logger.Info("[BOOT] process", zap.Int64("pid", processID))
	app := fx.New(
		fx.Provide(
			func() context.Context {
				return context.Background()
			},
		),
		fx.Provide(func() (*zap.Logger, error) {
			return zap.NewProduction()
			// или zap.NewDevelopment()
		}),
		postgres.Module(),
		repository.Module(),
		config.Module(),

		runner_old.Module(),
		okx_websocket.Module(),
		strategy.Module(),
		bootstrap.Module(),

		telegram.Module(),
		telegram_public.Module(),
		api.Module(),
		httpserver.Module(),
	)
	app.Run()
}
