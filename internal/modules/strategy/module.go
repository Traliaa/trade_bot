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
			var (
				runCtx context.Context
				cancel context.CancelFunc
			)

			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					runCtx, cancel = context.WithCancel(context.Background())

					go func() {
						log.Printf("[STRAT] hub loop started")
						defer log.Printf("[STRAT] hub loop stopped")

						for {
							select {
							case <-runCtx.Done():
								return
							case t, ok := <-ticks:
								if !ok {
									log.Printf("[STRAT] ticks channel closed")
									return
								}
								// важно: не передавать ctx OnStart
								s.OnTick(runCtx, t)
							}
						}
					}()

					return nil
				},
				OnStop: func(ctx context.Context) error {
					if cancel != nil {
						cancel()
					}
					return nil
				},
			})
		}),
	)
}
