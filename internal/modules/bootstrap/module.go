package bootstrap

import (
	"context"
	"log"
	bootstrap "trade_bot/internal/modules/bootstrap/service"
	"trade_bot/internal/modules/config"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("bootstrap",
		fx.Provide(
			bootstrap.NewWatchlist, // -> bootstrap.Watchlist
			bootstrap.NewWarmuper,  // -> bootstrap.Warmuper
		),
		fx.Invoke(func(lc fx.Lifecycle, cfg *config.Config, wl *bootstrap.OkxWatchlist, wu *bootstrap.Warmuper) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					// тут твой func1 скорее всего и был
					go func() {
						syms := wl.TopVolatile(cfg.Strategy.WatchTopN)
						if err := wu.Warmup(ctx, syms); err != nil {
							log.Printf("[BOOT] warmup error: %v", err)
							return
						}
						log.Printf("[BOOT] warmup done: %d symbols", len(syms))
					}()
					return nil
				},
			})
		}),
	)
}
