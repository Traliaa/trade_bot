package bootstrap

import (
	"context"
	"trade_bot/internal/base"

	"trade_bot/internal/modules/bootstrap/service"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/telegram_public/public"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

func Module() fx.Option {
	return fx.Module("bootstrap",
		fx.Provide(

			service.NewModuleConfig,
			service.NewService,
			fx.Annotate(
				func(s *public.Service) service.PublicNotifier { return s },
				fx.As(new(service.PublicNotifier)),
			),
		),
		fx.Invoke(func(lc fx.Lifecycle, root *zap.Logger, cfg *config.Config, s *service.Service) {
			s.Base = base.New("strategy", root, true)
			lc.Append(fx.Hook{
				OnStart: func(_ context.Context) error {
					return s.Start(context.Background())
				},
				OnStop: func(_ context.Context) error {
					s.Stop()
					return nil
				},
			})
		}),
	)
}
