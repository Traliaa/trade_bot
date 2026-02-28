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

func RunHTTP(lc fx.Lifecycle, cfg *config.Config, mux *chi.Mux) *http.Server {
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Service.PublicPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			fmt.Println("Starting HTTP server at", srv.Addr)
			go srv.Serve(ln)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})
	return srv

}

func Module() fx.Option {
	return fx.Module("health",
		fx.Provide(
			service.NewState,
			ProvideRouter,
			RunHTTP,
		),
		fx.Invoke(func(*http.Server) {}),
	)
}
