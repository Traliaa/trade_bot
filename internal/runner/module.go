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
			sigs chan models.Signal,
			ctx context.Context,
		) {
			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					go func() {
						for {
							select {
							case <-ctx.Done():
								return
							case sig := <-sigs:
								r.OnSignal(ctx, sig)
							}
						}
					}()
					return nil
				},
			})
		}),
	)
}
