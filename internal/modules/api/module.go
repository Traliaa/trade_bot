package api

import (
	"os"
	"trade_bot/internal/modules/api/controller"
	"trade_bot/internal/modules/api/middleware"
	"trade_bot/internal/runner/router"

	"go.uber.org/fx"

	"github.com/go-chi/chi/v5"
)

type Params struct {
	fx.In

	Router chi.Router
}

func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			provideBotToken,
			provideJWTSecret,

			controller.NewTgSessionController,
			controller.NewMeController,
			controller.NewHealthController,
			controller.NewTradeController,
		),
		fx.Invoke(
			func(t *controller.TradeController, r *router.Router) {
				t.SetRouter(r)
			},
			registerRoutes),
	)
}

func provideBotToken() string {
	// лучше потом заменить на твою config/rtc схему
	return os.Getenv("BOT_TOKEN")
}

func provideJWTSecret() []byte {
	// лучше потом заменить на твою config/rtc схему
	return []byte(os.Getenv("JWT_SECRET"))
}

func registerRoutes(p Params, tg *controller.TgSessionController, me *controller.MeController, jwtSecret []byte, health *controller.HealthController, trade *controller.TradeController) {

	p.Router.Route("/api", func(r chi.Router) {
		r.Use(middleware.CORS(middleware.CORSConfig{
			AllowedOrigins: []string{
				"http://localhost:5173",
				"https://trade.bot.etk3.xyz",
			},
		}))
		r.Post("/tg/session", tg.CreateSession)

		r.Group(func(pr chi.Router) {
			pr.Use(middleware.Auth(jwtSecret))
			pr.Get("/me", me.Me)
		})

		r.Route("/user/{id}", func(u chi.Router) {
			u.Post("/disable", trade.DisableUser)
			u.Post("/enable", trade.EnableUser)
			u.Post("/settings", trade.ApplySettings)
			u.Get("/status", trade.StatusForUser)
			u.Get("/settings", trade.GetSetting)
		})

		r.Post("/tune/auto", trade.AutoTuneNow)
		r.Post("/tune/toggle", trade.ToggleTuneMode)
		r.Get("/tune/mode", trade.TuneMode)
		r.Get("/tune/rejects", trade.StrategyRejects)
		r.Get("/tune/runtime", trade.StrategyTuning)

	})
	p.Router.Get("/live", health.Live)
}
