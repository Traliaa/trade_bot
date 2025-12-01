package main

import (
	"context"
	"log"
	"trade_bot/internal/config"
	"trade_bot/internal/exchange"
	"trade_bot/internal/notify"
	"trade_bot/internal/runner"
	"trade_bot/internal/strategy"

	"go.uber.org/fx"
)

func main() {
	app := fx.New(
		fx.Provide(
			config.Load,
			exchange.NewMexcClient,
			strategy.NewEMARSI,
			// Notifier: если TELEGRAM_* нет — используем stdout
			func(cfg *config.Config, mx *exchange.MexcClient) notify.Notifier {
				if cfg.TelegramBotToken != "" && cfg.TelegramChatID != 0 {
					if tg, err := notify.NewTelegram(cfg.TelegramBotToken, cfg.TelegramChatID, mx); err == nil {
						return tg
					}
				}
				return notify.NewStdout()
			},
			runner.New,
		),
		fx.Invoke(
			func(lc fx.Lifecycle, r *runner.Runner, n notify.Notifier) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						if tg, ok := n.(*notify.Telegram); ok {
							if err := tg.Start(ctx); err != nil {
								return err
							}
						}
						go r.Start(ctx)
						log.Println("runner started")
						return nil
					},
					OnStop: func(ctx context.Context) error {
						if tg, ok := n.(*notify.Telegram); ok {
							tg.Stop()
						}
						log.Println("stopping...")
						return nil
					},
				})
			},
		),
	)
	app.Run()
}
