package telegram_public

import (
	"context"
	"trade_bot/internal/modules/telegram_public/public"

	"go.uber.org/fx"
)

func Module() fx.Option {
	return fx.Module("public_telegram",
		fx.Provide(
			public.NewInMemoryRepo,
			fx.Annotate(func(r *public.InMemoryRepo) public.Repo { return r }, fx.As(new(public.Repo))),
			public.NewService,
		),
		fx.Invoke(func(lc fx.Lifecycle, svc *public.Service) {
			lc.Append(fx.Hook{
				OnStart: func(ctx context.Context) error {
					svc.Start(ctx)
					return nil
				},
				OnStop: func(ctx context.Context) error {
					svc.Stop()
					return nil
				},
			})
		}),
	)
}
