package api

import (
	"log"
	"net/http"
	"os"
	"trade_bot/internal/modules/api/controller"
	"trade_bot/internal/modules/api/middleware"

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
		),
		fx.Invoke(registerRoutes),
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

func registerRoutes(p Params, tg *controller.TgSessionController, me *controller.MeController, jwtSecret []byte, health *controller.HealthController) {
	log.Printf("api.registerRoutes: router=%p\n", p.Router)
	p.Router.Route("/api", func(r chi.Router) {
		r.Post("/tg/session", tg.CreateSession)

		r.Group(func(pr chi.Router) {
			pr.Use(middleware.Auth(jwtSecret))
			pr.Get("/me", me.Me)
		})

	})
	p.Router.Get("/live", health.Live)
	if routes, ok := any(p.Router).(chi.Routes); ok {
		_ = chi.Walk(routes, func(method string, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
			log.Printf("ROUTE %s %s", method, route)
			return nil
		})
	}
}
