package api

import (
	"os"

	"trade_bot/internal/modules/api/controller"
	"trade_bot/internal/modules/api/middleware"
	"trade_bot/internal/modules/runner/service"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
)

type Params struct {
	fx.In

	Router chi.Router
}

func Module() fx.Option {
	return fx.Module("api",
		fx.Provide(

			provideBotToken,
			provideJWTSecret,

			controller.NewTgSessionController,
			controller.NewMeController,
			controller.NewHealthController,
			controller.NewTradeController,
		),
		fx.Invoke(
			func(t *controller.TradeController, r *service.Service) {
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

func registerRoutes(
	p Params,
	tg *controller.TgSessionController,
	me *controller.MeController,
	jwtSecret []byte,
	health *controller.HealthController,
	trade *controller.TradeController,
) {
	p.Router.Get("/health", health.Live)

	p.Router.Route("/api", func(r chi.Router) {
		r.Use(middleware.CORS(middleware.CORSConfig{
			AllowedOrigins: []string{
				"http://localhost:5173",
				"https://trade.bot.etk3.xyz",
			},
			AllowedHeaders: []string{
				"Authorization",
				"Content-Type",
			},
			AllowCredentials: true,
		}))

		r.Post("/tg/session", tg.CreateSession)

		r.Group(func(pr chi.Router) {
			pr.Use(middleware.Auth(jwtSecret))

			pr.Get("/me", me.Me)

			pr.Post("/bot/enable", trade.EnableUser)
			pr.Post("/bot/disable", trade.DisableUser)

			pr.Get("/settings", trade.GetSetting)
			pr.Post("/settings", trade.ApplySettings)

			pr.Get("/status", trade.StatusForUser)

			pr.Get("/trades", trade.RecentTrades)
			pr.Get("/stats", trade.TradeStats)

			pr.Route("/strategy", func(sr chi.Router) {
				sr.Get("/runtime", trade.StrategyTuning)
				sr.Get("/rejects", trade.StrategyRejects)

				sr.Route("/tune", func(tr chi.Router) {
					tr.Post("/auto", trade.AutoTuneNow)
					tr.Post("/toggle", trade.ToggleTuneMode)
					tr.Get("/mode", trade.TuneMode)
				})
			})
		})
	})
}
