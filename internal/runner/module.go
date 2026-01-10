package runner

import (
	"context"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/runner/router"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("runner",
		fx.Provide(
			router.NewRouter, // *Router
		),
		fx.Invoke(func(
			lc fx.Lifecycle,
			r *router.Router,
			sigs chan models.Signal, // ⬅️ read-only
			candles chan models.CandleTick, // канал для стопов
		) {
			lc.Append(fx.Hook{
				OnStart: func(startCtx context.Context) error {
					runCtx, cancel := context.WithCancel(context.Background())

					// stop
					lc.Append(fx.Hook{
						OnStop: func(_ context.Context) error {
							cancel()
							return nil
						},
					})
					go func() {
						for {
							select {
							case <-runCtx.Done():
								return
							case sig, ok := <-sigs:
								if !ok {
									return
								}
								r.OnSignal(runCtx, sig)
							}
						}
					}()

					agg := router.NewCandleAgg()

					// #1 reader
					go func() {
						for {
							select {
							case <-runCtx.Done():
								return
							case ct, ok := <-candles:
								if !ok {
									return
								}
								if helper.NormTF(ct.TimeframeRaw) != "1m" {
									continue
								}
								agg.Put(ct)
							}
						}
					}()

					// #2 periodic worker
					go func() {
						ticker := time.NewTicker(1 * time.Second)
						defer ticker.Stop()

						sem := make(chan struct{}, 4)

						for {
							select {
							case <-runCtx.Done():
								return
							case <-ticker.C:
								batch := agg.Drain()

								for _, ct := range batch {
									ct := ct
									select {
									case sem <- struct{}{}:
										go func() {
											defer func() { <-sem }()
											r.OnCandleClose(runCtx, ct)
										}()
									default:
										// перегруз — пропускаем
									}
								}
							}
						}
					}()
					return nil
				},
			})
		}),
	)
}
