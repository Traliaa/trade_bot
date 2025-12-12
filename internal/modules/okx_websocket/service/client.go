package service

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"

	"github.com/gorilla/websocket"
)

type ServiceNotifier interface {
	SendService(ctx context.Context, format string, args ...any)
}

type Client struct {
	cfg *config.Config
	n   ServiceNotifier

	http      *http.Client
	wsDialer  *websocket.Dialer
	apiKey    string
	apiSecret string
	passph    string

	mu    sync.RWMutex
	subs  map[string]map[chan models.CandleTick]struct{}
	watch []string // общий watchlist, который мы стримим
}

func NewClient(cfg *config.Config, n ServiceNotifier) *Client {
	return &Client{
		wsDialer:  &websocket.Dialer{},
		http:      &http.Client{Timeout: 10 * time.Second},
		cfg:       cfg,
		apiKey:    cfg.OKXAPIKey,
		apiSecret: cfg.OKXAPISecret,
		passph:    cfg.OKXPassphrase,
		n:         n,
		subs:      make(map[string]map[chan models.CandleTick]struct{}),
		watch:     nil,
	}
}

// OutTick — что отдаём наружу (стрим в StrategyHub).
type OutTick struct {
	InstID    string
	Timeframe string
	Candle    models.CandleTick // или твой CandleTick с OHLCV
}

// Start собирает топ-волатильные и стримит по нескольким таймфреймам.
func (c *Client) Start(ctx context.Context, out chan<- OutTick) {
	if c.n != nil {
		c.n.SendService(ctx, "🚀 OKX WebSocket streamer started (5m/10m/15m)")
	}

	// 1. Берём топ N самых волатильных
	syms := c.TopVolatile(c.cfg.DefaultWatchTopN)
	if len(syms) == 0 {
		log.Println("[MARKET] пустой список волатильных инструментов")
		return
	}
	//timeframes := []string{"1m", "5m", "15m"}

	timeframes := []string{"15m"}

	for _, tf := range timeframes {
		tf := tf
		go c.runTimeframe(ctx, tf, syms, out)
	}
}
func (c *Client) runTimeframe(
	ctx context.Context,
	timeframe string,
	syms []string,
	out chan<- OutTick,
) {
	if c.n != nil {
		c.n.SendService(ctx, "[MARKET] ▶️ WS connect %s %d symbols", timeframe, len(syms))
	}

	ticks := c.StreamCandlesBatch(ctx, syms, timeframe)

	for {
		select {
		case <-ctx.Done():
			if c.n != nil {
				c.n.SendService(ctx, "[MARKET] ⏹ stop %s", timeframe)
			}
			return

		case tick, ok := <-ticks:
			if !ok {
				if c.n != nil {
					c.n.SendService(ctx, "[MARKET] ❌ stream closed %s", timeframe)
				}
				return
			}

			// debug-лог по каждому тику
			log.Printf("[WS-TICK] %s %s close=%.6f", tick.InstID, timeframe, tick.Close)

			// прокидываем дальше
			candle := models.CandleTick{
				Open:   tick.Open,
				High:   tick.High,
				Low:    tick.Low,
				Close:  tick.Close,
				Volume: tick.Volume,
				Start:  tick.Start,
				End:    tick.End,
			}

			select {
			case out <- OutTick{
				InstID:    tick.InstID,
				Timeframe: timeframe,
				Candle:    candle,
			}:
				// ok
			case <-ctx.Done():
				return
			}
		}
	}
}
