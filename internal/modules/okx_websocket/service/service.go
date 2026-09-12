package service

import (
	"context"
	"net/http"
	"sync"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"

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
	endpoint        string
	ServiceNotifier ServiceNotifier
	client          http.Client
	wsDialer        *websocket.Dialer

	mu                 sync.RWMutex
	subs               map[string]map[chan models.CandleTick]struct{}
	watch              []string // общий watchlist, который мы стримим
	marketLastSeen     map[string]time.Time
	instrumentLastSeen map[string]map[string]time.Time
	dynamicRequested   int
	selectedSince      map[string]time.Time
	rotationReady      func() bool
	prepareRotation    func(context.Context, []string) error
	rotationAt         time.Time
	rotationError      string
	retainedCount      int

	cfg       *config.Config
	apiKey    string
	apiSecret string
	passph    string
}

// NewService ...
func NewService(params Params, n ServiceNotifier) *Service {
	return &Service{
		endpoint:  "https://www.okx.com/api/v5/market",
		cfg:       params.Config.cfg,
		apiKey:    params.Config.cfg.OKXWS.APIKey,
		apiSecret: params.Config.cfg.OKXWS.APISecret,
		passph:    params.Config.cfg.OKXWS.Passphrase,

		ServiceNotifier:  n,
		wsDialer:         &websocket.Dialer{},
		client:           http.Client{Timeout: 10 * time.Second},
		subs:             make(map[string]map[chan models.CandleTick]struct{}),
		watch:            nil,
		dynamicRequested: params.Config.cfg.Strategy.WatchTopN,
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

		var symbols []string
		for {
			r, err := s.rankedUniverse(ctx, s.cfg.Strategy.WatchTopN, models.UniverseConservative)
			if err == nil && len(r.Selected) > 0 {
				for _, c := range r.Selected {
					symbols = append(symbols, c.Symbol)
				}
				break
			}
			s.mu.Lock()
			s.rotationError = "Не удалось выбрать монеты для анализа; повтор через 30 секунд"
			s.mu.Unlock()
			s.Logger.Warn("initial universe unavailable", zap.Error(err))
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
		s.mu.Lock()
		s.watch = append([]string(nil), symbols...)
		s.rotationError = ""
		s.selectedSince = map[string]time.Time{}
		for _, symbol := range symbols {
			s.selectedSince[symbol] = time.Now()
		}
		s.mu.Unlock()

		timeframes := uniqTimeframes("1m", s.cfg.Strategy.LTF, s.cfg.Strategy.HTF)

		s.Logger.Info("market streams starting", zap.Int("instruments", len(symbols)))

		for _, tf := range timeframes {
			go s.runTimeframe(ctx, toOKXBar(tf), symbols, out)
		}
		s.runRotation(ctx, symbols, out)
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
