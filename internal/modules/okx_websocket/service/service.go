package service

import (
	"context"
	"net/http"
	"sync"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/telegram_public/public"

	"github.com/gorilla/websocket"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Params - parameters.
type Params struct {
	fx.In
	Config *ModuleConfig
}

type ServiceNotifier interface {
	SendServiceText(ctx context.Context, text string) (messageID int, err error)
	EditServiceText(ctx context.Context, messageID int, text string) error
}

// Service ...
type Service struct {
	base.Base
	ServiceNotifier ServiceNotifier
	http            *http.Client
	wsDialer        *websocket.Dialer

	mu    sync.RWMutex
	subs  map[string]map[chan models.CandleTick]struct{}
	watch []string // общий watchlist, который мы стримим

	cfg       *config.Config
	apiKey    string
	apiSecret string
	passph    string
}

// NewService ...
func NewService(params Params, n ServiceNotifier) *Service {
	return &Service{
		cfg:       params.Config.cfg,
		apiKey:    params.Config.cfg.OKXWS.APIKey,
		apiSecret: params.Config.cfg.OKXWS.APISecret,
		passph:    params.Config.cfg.OKXWS.Passphrase,

		ServiceNotifier: n,
		wsDialer:        &websocket.Dialer{},
		http:            &http.Client{Timeout: 10 * time.Second},
		subs:            make(map[string]map[chan models.CandleTick]struct{}),
		watch:           nil,
	}
}

// Start ...
func (s *Service) Start(ctx context.Context, out chan<- models.CandleTick) error {
	ctx, shouldStart, started, stopped := s.StartInit(ctx)
	if !shouldStart {
		return nil
	}

	go func() {
		started()
		defer stopped()

		s.Logger.Debug("Цикл запуска начат")
		defer s.Logger.Debug("Цикл запуска остановлен")

		var err error

		s.cfg.Strategy.Symbols, err = s.TopVolatile(s.cfg.Strategy.WatchTopN)
		if err != nil {
			s.Logger.Info("[MARKET] ошибка TopVolatile: %v", zap.Error(err))
			return
		}
		if len(s.cfg.Strategy.Symbols) == 0 {
			s.Logger.Info("[MARKET] пустой список волатильных инструментов")
			return
		}

		timeframes := uniqTimeframes("1m", s.cfg.Strategy.LTF, s.cfg.Strategy.HTF)

		_, _ = s.ServiceNotifier.SendServiceText(ctx, public.Status{
			State:       public.StateConnecting,
			Exchange:    "OKX",
			Instruments: len(s.cfg.Strategy.Symbols),
			UpdatedAt:   time.Now(),
		}.RenderHTML())

		for _, tf := range timeframes {
			go s.runTimeframe(ctx, toOKXBar(tf), s.cfg.Strategy.Symbols, out)
		}
	}()
	return nil
}

func (s *Service) runTimeframe(
	ctx context.Context,
	okxBar string,
	syms []string,
	out chan<- models.CandleTick,
) {
	ticks := s.StreamCandlesBatch(ctx, syms, okxBar) // <-- ВАЖНО: okxBar

	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-ticks:
			if !ok {
				return
			}

			candle := models.CandleTick{
				Open:         tick.Open,
				High:         tick.High,
				Low:          tick.Low,
				Close:        tick.Close,
				Volume:       tick.Volume,
				Start:        tick.Start,
				End:          tick.End,
				InstID:       tick.InstID,
				TimeframeRaw: tick.TimeframeRaw,
			}

			s.Logger.Debug("okx ws publish candle",
				zap.String("instId", candle.InstID),
				zap.String("tf", candle.TimeframeRaw),
			)
			select {
			case out <- candle:
			case <-ctx.Done():
				return
			}
		}
	}
}
