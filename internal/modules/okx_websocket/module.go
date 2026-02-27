package okx_websocket

import (
	"context"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/okx_websocket/service"
	telegram "trade_bot/internal/modules/telegram_bot/service"

	"go.uber.org/fx"
)

func newOutTickChan() chan models.CandleTick {
	return make(chan models.CandleTick, 4096)
}

// 👇 вот этого не хватало: Provide receive-only канал как отдельный тип
func asRecvOnly(ch chan models.CandleTick) <-chan models.CandleTick { return ch }

func Module() fx.Option {
	return fx.Module("okx_websocket",
		fx.Provide(
			service.NewClient,
			newOutTickChan, // chan service.OutTick
			asRecvOnly,     // <-chan service.OutTick
			fx.Annotate(
				func(s *telegram.Telegram) service.ServiceNotifier { return s },
				fx.As(new(service.ServiceNotifier)),
			),
		),
		fx.Invoke(func(lc fx.Lifecycle, s *service.Client, out chan models.CandleTick) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go s.Start(ctx, out) // Start ждёт chan<- -> сюда подходит chan
					return nil
				},
			})
		}),
	)
}
