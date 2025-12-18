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

func newOutTickChan() chan service.OutTick {
	return make(chan service.OutTick, 4096)
}

// 👇 вот этого не хватало: Provide receive-only канал как отдельный тип
func asRecvOnly(ch chan service.OutTick) <-chan service.OutTick { return ch }

func Module() fx.Option {
	return fx.Module("okx_websocket",
		fx.Provide(
			service.NewClient,
			newOutTickChan, // chan service.OutTick
			asRecvOnly,     // <-chan service.OutTick
		),
		fx.Invoke(func(lc fx.Lifecycle, s *service.Client, out chan service.OutTick) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go s.Start(ctx, out) // Start ждёт chan<- -> сюда подходит chan
					return nil
				},
			})
		}),
	)
}
