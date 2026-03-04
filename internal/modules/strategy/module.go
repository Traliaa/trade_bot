package strategy

import (
	"context"
	"trade_bot/internal/base"

	"trade_bot/internal/modules/strategy/service"

	"go.uber.org/fx"
	"go.uber.org/zap"

	"trade_bot/internal/models"
)

func newSignalsChan() chan models.Signal {
	return make(chan models.Signal, 4096)
}
func asSendOnlySignals(ch chan models.Signal) chan<- models.Signal { return ch }

func asSendOnlyStopSignals(ch chan models.CandleTick) chan<- models.CandleTick { return ch }
func Module() fx.Option {
	return fx.Module("strategy",
		fx.Provide(

			newSignalsChan,    // chan models.Signal
			asSendOnlySignals, // chan<- models.Signal
			asSendOnlyStopSignals,
			service.NewService, // *service.Hub (получит V2Config, Notifier, chan<-Signal, Engine)

		),

		fx.Invoke(func(lc fx.Lifecycle, root *zap.Logger, s *service.Service, ticks <-chan models.CandleTick) {
			s.Base = base.New("strategy", root, true)
			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					return s.Start(context.Background(), ticks)
				},
				OnStop: func(_ context.Context) error {
					s.Stop()
					return nil
				},
			})
		}),
	)
}
