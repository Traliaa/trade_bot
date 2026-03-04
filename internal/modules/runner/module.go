package runner

import (
	"context"
	"trade_bot/internal/base"

	"trade_bot/internal/models"
	runner "trade_bot/internal/modules/runner/service"
	"trade_bot/internal/modules/telegram_bot/service"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func Module() fx.Option {
	return fx.Module("runner",
		fx.Provide(

			runner.NewModuleConfig,
			runner.NewService,
			fx.Annotate(
				func(s *service.Telegram) runner.TelegramNotifier { return s },
				fx.As(new(runner.TelegramNotifier)),
			),
		),

		fx.Invoke(func(lc fx.Lifecycle, s *runner.Service, root *zap.Logger, sigs chan models.Signal, // входящие сигналы
			candles chan models.CandleTick) {
			s.Base = base.New("runner", root, true)

			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					return s.Start(context.Background(), sigs, candles)
				},
				OnStop: func(_ context.Context) error {
					s.Stop()
					return nil
				},
			})
		}),
	)
}
