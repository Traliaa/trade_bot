package strategy

import (
	"context"
	"log"
	"trade_bot/internal/modules/strategy/service"

	"go.uber.org/fx"

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

		fx.Invoke(func(lc fx.Lifecycle, s *service.Service, ticks <-chan models.CandleTick) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					go func() {
						log.Printf("[STRAT] hub loop started")
						for {
							select {
							case <-ctx.Done():
								log.Printf("[STRAT] hub loop stopped")
								return
							case t, ok := <-ticks:
								if !ok {
									log.Printf("[STRAT] ticks channel closed")
									return
								}
								s.OnTick(ctx, t)
							}
						}
					}()
					return nil
				},
			})
		}),
	)
}
