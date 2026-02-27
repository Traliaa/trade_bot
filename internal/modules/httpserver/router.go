package httpserver

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"
	"trade_bot/internal/modules/config"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
)

type HookParams struct {
	fx.In
	Lifecycle fx.Lifecycle
	Server    *http.Server
}

func RegisterHooks(p HookParams) {
	log.Println("httpserver: RegisterHooks called")

	var ln net.Listener

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Printf("httpserver: OnStart begin addr=%s", p.Server.Addr)

			l, err := net.Listen("tcp", p.Server.Addr)
			if err != nil {
				log.Printf("httpserver: bind error: %v", err)
				return err
			}
			ln = l

			log.Printf("httpserver: LISTEN OK %s", p.Server.Addr)

			go func() {
				err := p.Server.Serve(ln)
				log.Printf("httpserver: Serve returned: %v", err)
				if err != nil && err != http.ErrServerClosed {
					log.Printf("httpserver: serve error: %v", err)
				}
			}()

			return nil
		},

		OnStop: func(ctx context.Context) error {
			log.Println("httpserver: OnStop")
			_ = p.Server.Shutdown(ctx)
			if ln != nil {
				_ = ln.Close()
			}
			return nil
		},
	})
}

type RouterOut struct {
	fx.Out
	Mux    *chi.Mux
	Router chi.Router
}

func ProvideRouter() RouterOut {
	mux := chi.NewRouter()
	return RouterOut{Mux: mux, Router: mux}
}

type ServerParams struct {
	fx.In
	Handler *chi.Mux
	Cfg     *config.Config
}

func RunHTTPServer(p ServerParams) *http.Server {
	addr := fmt.Sprintf(":%d", p.Cfg.Service.PublicPort)
	log.Printf("httpserver: create server addr=%s handler=%p", addr, p.Handler)

	return &http.Server{
		Addr:              addr,
		Handler:           p.Handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
}

func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			ProvideRouter,
			RunHTTPServer,
		),
		fx.Invoke(RegisterHooks),
	)
}
