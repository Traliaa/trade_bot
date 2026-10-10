package telegram

import (
	"trade_bot/internal/modules/bootstrap/lifecyclelog"
	runner "trade_bot/internal/modules/runner_old/service"
	"trade_bot/internal/modules/telegram_bot/service"
	"trade_bot/internal/modules/telegram_public/public"

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
			func(t *service.Telegram, r *runner.Service) {
				t.SetRouter(r)
			},
			func(lc fx.Lifecycle, t *service.Telegram) {
				lc.Append(lifecyclelog.WrapHook("telegram", receiverHook(t)))
			},
		),
	)
}
