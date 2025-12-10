package okx_websocket

import (
	"context"
	"trade_bot/internal/modules/okx_websocket/service"

	"go.uber.org/fx"
)

// этот интерфейс должен реализовать твой Telegram-сервис
type ServiceNotifier interface {
	SendService(ctx context.Context, format string, args ...any)
}

// Module поднимает стример свечей OKX.
func Module() fx.Option {
	return fx.Module("okx_websocket",
		fx.Provide(
			service.NewClient, // *service.Stream (или как у тебя называется)
			func() chan service.OutTick {
				// общий буфер для свечей
				return make(chan service.OutTick, 1024)
			},
		),
		fx.Invoke(func(lc fx.Lifecycle, s *service.Client, out chan service.OutTick) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go s.Start(ctx, out) // 👈 теперь передаём out
					return nil
				},
				OnStop: func(ctx context.Context) error {
					// если нужно — закрыть канал/остановить стрим
					// close(out)  // только если больше никто не пишет
					return nil
				},
			})
		}),
	)
}
