package api

import (
	"log"
	"os"

	"trade_bot/internal/modules/api/controller"
	"trade_bot/internal/modules/api/middleware"
	"trade_bot/internal/modules/runner_old/service"

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

			provideJWTSecret,

			controller.NewTgSessionController,
			controller.NewMeController,
			controller.NewHealthController,
			controller.NewTradeController,
			controller.NewServiceStatusController,
		),
		fx.Invoke(
			func(t *controller.TradeController, r *service.Service) {
				t.SetRouter(r)
			},
			registerRoutes),
	)
}

func provideJWTSecret() []byte {
	// лучше потом заменить на твою config/rtc схему
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))

	if len(jwtSecret) == 0 {
		log.Fatal("JWT_SECRET is empty")
	}
	return jwtSecret
}

func registerRoutes(
	p Params,
	tg *controller.TgSessionController,
	me *controller.MeController,
	jwtSecret []byte,
	health *controller.HealthController,
	trade *controller.TradeController,
	serviceStatus *controller.ServiceStatusController,
) {
	p.Router.Get("/health", health.Live)
	p.Router.Get("/ready", serviceStatus.Ready)

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
		r.Post("/dev/session", tg.CreateDevSession)
		r.Group(func(pr chi.Router) {
			pr.Use(middleware.Auth(jwtSecret))

			pr.Get("/me", me.Me)

			pr.Post("/bot/enable", trade.EnableUser)
			pr.Post("/bot/disable", trade.DisableUser)

			pr.Get("/settings", trade.GetSetting)
			pr.Post("/settings", trade.ApplySettings)
			pr.Post("/connection/okx", trade.SaveOKXKeys)

			pr.Get("/status", trade.StatusForUser)
			pr.Get("/service/status", serviceStatus.Get)

			pr.Get("/positions", trade.Positions)
			pr.Get("/open_trades", trade.OpenTrades)
			pr.Get("/trades", trade.RecentTrades)
			pr.Get("/trades/{guid}/fills", trade.TradeFills)
			pr.Post("/trades/{guid}/close", trade.ManualClose)
			pr.Get("/trades/{guid}/close/{requestID}", trade.ManualCloseStatus)
			pr.Get("/stats", trade.TradeStats)

			pr.Route("/strategy", func(sr chi.Router) {
				sr.Use(middleware.RequireAdmin)
				sr.Get("/runtime", trade.StrategyTuning)
				sr.Get("/rejects", trade.StrategyRejects)
				sr.Get("/execution", trade.ExecutionStats)

				sr.Route("/tune", func(tr chi.Router) {
					tr.Post("/auto", trade.AutoTuneNow)
					tr.Post("/toggle", trade.ToggleTuneMode)
					tr.Get("/mode", trade.TuneMode)
				})
			})
		})
	})
}
