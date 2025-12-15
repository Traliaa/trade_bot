package health

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"go.uber.org/fx"

	"trade_bot/internal/modules/health/service"
)

type Config struct {
	Addr string // например ":8080"
}

func NewConfig() Config {
	return Config{Addr: ":8080"}
}

func NewMux(state *service.State) *http.ServeMux {
	mux := http.NewServeMux()

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

	return mux
}

func RunHTTP(lc fx.Lifecycle, cfg Config, mux *http.ServeMux) {
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", cfg.Addr)
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
			NewConfig,
			NewMux,
		),
		fx.Invoke(RunHTTP),
	)
}
