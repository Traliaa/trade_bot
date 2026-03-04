package telegram

import (
	"context"
	"fmt"

	"trade_bot/internal/modules/bootstrap/lifecyclelog"
	runner "trade_bot/internal/modules/runner/service"
	"trade_bot/internal/modules/telegram_bot/service"
	"trade_bot/internal/modules/telegram_public/public"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func Module() fx.Option {
	return fx.Module("telegram",
		fx.Provide(

			service.NewTelegram,
			fx.Annotate(
				func(t *service.Telegram) public.PublicNotifier { return t },
				fx.As(new(public.PublicNotifier)),
			),
		),
		fx.Invoke(
			func(t *service.Telegram, r *runner.Service) {
				t.SetRouter(r)
			},
			func(lc fx.Lifecycle, t *service.Telegram) {
				lc.Append(lifecyclelog.WrapHook("telegram", fx.Hook{
					OnStart: func(ctx context.Context) error {
						go func() {
							if err := t.Start(ctx); err != nil && ctx.Err() == nil {
								fmt.Errorf("telegram run", zap.Error(err)) // или log.Printf
							}
						}()
						return nil
					},

					OnStop: func(ctx context.Context) error {
						t.Stop()
						return nil
					},
				}))
			},
		),
	)
}
