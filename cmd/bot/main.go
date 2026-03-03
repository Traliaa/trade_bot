package main

import (
	"context"
	"os"
	"time"
	"trade_bot/internal/modules/api"
	"trade_bot/internal/modules/bootstrap"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/httpserver"
	"trade_bot/internal/modules/okx_websocket"
	"trade_bot/internal/modules/postgres"
	"trade_bot/internal/modules/repository"
	"trade_bot/internal/modules/strategy"
	telegram "trade_bot/internal/modules/telegram_bot"
	"trade_bot/internal/modules/telegram_public"
	"trade_bot/internal/runner"
	"trade_bot/pkg/logger"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
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

		postgres.Module(),
		repository.Module(),
		config.Module(),

		runner.Module(),
		okx_websocket.Module(),
		strategy.Module(),
		bootstrap.Module(),

		telegram.Module(),
		telegram_public.Module(),
		api.Module(),
		httpserver.Module(),
		fx.WithLogger(func() fxevent.Logger {
			return &fxevent.ConsoleLogger{W: os.Stdout}
		}),
		fx.StartTimeout(60*time.Second),
		fx.StopTimeout(30*time.Second),
	)
	app.Run()
}
