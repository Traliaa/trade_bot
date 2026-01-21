package telegram

import (
	"context"
	okx_websocket "trade_bot/internal/modules/okx_websocket/service"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/modules/telegram_bot/service/pg"

	"trade_bot/internal/modules/telegram_bot/service"
	"trade_bot/internal/runner"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("telegram",
		// 1. Репозиторий юзеров
		fx.Provide(
			pg.NewUser, // func(...) *pg.User
		),

		// 2. Сервис Telegram как *service.Telegram
		fx.Provide(
			service.NewTelegram, // func(*config.Config, *pg.User, *runner.Manager) (*service.Telegram, error)
			func(t *service.Telegram) okx_websocket.ServiceNotifier {
				return t
			},
			func(t *service.Telegram) strategy.ServiceNotifier {
				return t
			},
		),

		// 3. Адаптер: *service.Telegram -> runner.TelegramNotifier
		fx.Provide(
			func(t *service.Telegram) runner.TelegramNotifier {
				return t
			},
		),
		// Запуск основного цикла через Lifecycle
		fx.Invoke(
			func(lc fx.Lifecycle, t *service.Telegram) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						t.Start(ctx)
						return nil
					},
					OnStop: func(ctx context.Context) error {
						t.Stop()
						return nil
					},
				})
			},
		),
	)
}
