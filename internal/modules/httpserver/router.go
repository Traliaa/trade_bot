package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/httpserver/service"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
)

type RouterOut struct {
	fx.Out
	Mux    *chi.Mux
	Router chi.Router
}

func ProvideRouter(state *service.State) RouterOut {
	mux := chi.NewRouter()

	mux.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) {
		// liveness: процесс жив
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		// readiness: сервис готов обслуживать трафик
		if !state.Ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// полезный JSON для отладки
		resp := map[string]any{
			"ready":       state.Ready(),
			"wsConnected": state.WSConnected(),
			"uptimeSec":   int64(state.Uptime().Seconds()),
			"lastTickUnix": func() int64 {
				t := state.LastTick()
				if t.IsZero() {
					return 0
				}
				return t.Unix()
			}(),
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return RouterOut{Mux: mux, Router: mux}
}

type ServerParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	Handler   *chi.Mux
}

//
//func RunHTTPServer(p ServerParams, cfg *config.Config) *http.Server {
//	log.Printf("httpserver: handler=%p\n", p.Handler)
//	srv := &http.Server{
//		Addr:              fmt.Sprintf(":%d", cfg.Service.PublicPort),
//		Handler:           p.Handler,
//		ReadHeaderTimeout: 5 * time.Second,
//	}
//	return srv
//}
//
//func Module() fx.Option {
//	return fx.Options(
//		fx.Provide(
//			ProvideRouter,
//			RunHTTPServer,
//		),
//
//		fx.Invoke(func(lc fx.Lifecycle, srv *http.Server) {
//			log.Println("httpserver: invoke hook install") // <-- добавь
//			log.Printf("httpserver: listening on %s", srv.Addr)
//
//			lc.Append(fx.Hook{
//				OnStart: func(ctx context.Context) error {
//					go func() {
//						if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
//							log.Printf("httpserver: listen error: %v", err)
//						}
//					}()
//					return nil
//				},
//				OnStop: func(ctx context.Context) error {
//					return srv.Shutdown(ctx)
//				},
//			})
//		}),
//	)
//}

func RunHTTP(lc fx.Lifecycle, cfg *config.Config, mux *chi.Mux) {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Service.PublicPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Service.PublicPort))
			if err != nil {
				return err
			}
			go func() { _ = srv.Serve(ln) }()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
}

func Module() fx.Option {
	return fx.Module("health",
		fx.Provide(
			service.NewState,
			ProvideRouter,
		),
		fx.Invoke(RunHTTP),
	)
}
