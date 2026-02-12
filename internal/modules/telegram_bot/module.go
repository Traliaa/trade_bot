package telegram

import (
	"context"
	"trade_bot/internal/modules/telegram_public/public"
	"trade_bot/internal/runner/router"

	"trade_bot/internal/modules/telegram_bot/service"

	"go.uber.org/fx"
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
			func(t *service.Telegram, r *router.Router) {
				t.SetRouter(r)
			},
			func(lc fx.Lifecycle, t *service.Telegram) {
				lc.Append(fx.Hook{
					OnStart: func(ctx context.Context) error {
						t.Start(ctx)
						return nil
					},

					OnStop: func(ctx context.Context) error {
						t.Stop()
						return nil
					},
				})
			},
		),
	)
}
