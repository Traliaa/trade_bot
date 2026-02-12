package runner

import (
	"context"
	"time"

	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/telegram_bot/service"
	"trade_bot/internal/modules/telegram_bot/service/pg"
	"trade_bot/internal/runner/router"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("runner",
		fx.Provide(
			router.NewRouter, // *Router
			fx.Annotate(
				func(s *service.Telegram) router.TelegramNotifier { return s },
				fx.As(new(router.TelegramNotifier)),
			),
			fx.Annotate(
				func(s *pg.User) router.Repository { return s },
				fx.As(new(router.Repository)),
			),
		),

		// ✅ Восстановление активных пользователей при старте сервиса
		fx.Invoke(func(lc fx.Lifecycle, r *router.Router, repo *pg.User, tg *service.Telegram) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					r.RestoreEnabled(ctx)
					return nil
				},
			})
		}),

		// ✅ Основной раннер сигналов/свечей
		fx.Invoke(func(
			lc fx.Lifecycle,
			r *router.Router,
			sigs chan models.Signal, // входящие сигналы
			candles chan models.CandleTick, // канал для агрегации свечей
		) {
			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					runCtx, cancel := context.WithCancel(context.Background())

					lc.Append(fx.Hook{
						OnStop: func(_ context.Context) error {
							cancel()
							return nil
						},
					})

					// #0 signals router
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
