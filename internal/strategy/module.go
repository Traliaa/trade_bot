package strategy

import (
	"context"

	"go.uber.org/fx"

	"trade_bot/internal/models"
	okxws "trade_bot/internal/modules/okx_websocket/service"
)

func Module() fx.Option {
	return fx.Module("strategy",
		fx.Provide(
			// общий канал сигналов для всех стратегий
			func() chan models.Signal {
				return make(chan models.Signal, 1024)
			},
			NewHub, // func(out chan<- models.Signal) *Hub
		),
		fx.Invoke(func(
			lc fx.Lifecycle,
			hub *Hub,
			ticks chan okxws.OutTick, // от WS-модуля
			ctx context.Context,
		) {
			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					go func() {
						for {
							select {
							case <-ctx.Done():
								return
							case t := <-ticks:
								// тут вызываем стратегию
								hub.OnCandle(ctx, t.InstID, t.Timeframe, t.Candle)
							}
						}
					}()
					return nil
				},
			})
		}),
	)
}
