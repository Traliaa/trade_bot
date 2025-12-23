package runner

import (
	"context"
	"trade_bot/internal/models"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("runner",
		fx.Provide(
			NewRouter, // *Router
		),
		fx.Invoke(func(
			lc fx.Lifecycle,
			r *Router,
			sigs chan models.Signal, // ⬅️ read-only
		) {
			lc.Append(fx.Hook{
				OnStart: func(startCtx context.Context) error {
					go func() {
						for {
							select {
							case <-startCtx.Done():
								return
							case sig, ok := <-sigs:
								if !ok {
									return
								}
								r.OnSignal(startCtx, sig)
							}
						}
					}()
					return nil
				},
			})
		}),
	)
}
