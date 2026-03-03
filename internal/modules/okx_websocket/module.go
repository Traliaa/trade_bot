package okx_websocket

import (
	"context"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/okx_websocket/service"
	telegram "trade_bot/internal/modules/telegram_bot/service"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func newOutTickChan() chan models.CandleTick {
	return make(chan models.CandleTick, 4096)
}

// 👇 вот этого не хватало: Provide receive-only канал как отдельный тип
func asRecvOnly(ch chan models.CandleTick) <-chan models.CandleTick { return ch }

func Module() fx.Option {
	return fx.Module("okx_websocket",
		fx.Provide(
			fx.Annotate(
				func(s *telegram.Telegram) service.ServiceNotifier { return s },
				fx.As(new(service.ServiceNotifier)),
			),
			service.NewModuleConfig,
			service.NewService,

			newOutTickChan, // chan service.OutTick
			asRecvOnly,     // <-chan service.OutTick

		),
		fx.Invoke(func(lc fx.Lifecycle, s *service.Service, root *zap.Logger, out chan models.CandleTick) {
			s.Base = base.New("okx_websocket", root, true)

			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					return s.Start(context.Background(), out)
				},
				OnStop: func(_ context.Context) error {
					s.Stop()
					return nil
				},
			})
		}),
	)
}
