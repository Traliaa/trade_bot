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
	// 1. общий watchlist
	raw := c.TopVolatile(c.cfg.DefaultWatchTopN)

	watch := make([]string, 0, len(raw))
	for _, inst := range raw {
		if c.HasCandles(inst, "5m") { // проверка на один ТФ достаточно
			watch = append(watch, inst)
		}
	}
	if len(watch) == 0 {
		log.Println("[MARKET] watchlist пуст")
		c.n.SendService(ctx, "❌ MARKET: watchlist пуст, свечи не запущены")
		return
	}

	c.n.SendService(ctx, "🟢 MARKET: старт %d символов (5/10/15m)", len(watch))

	timeframes := []string{"5m", "10m", "15m"}

	for _, tf := range timeframes {
		tf := tf
		go c.runOneTF(ctx, tf, watch, out)
	}
}

func (c *Client) runOneTF(ctx context.Context, tf string, watch []string, out chan<- OutTick) {
	for {
		select {
		case <-ctx.Done():
			c.n.SendService(ctx, "🔴 MARKET[%s]: остановлен", tf)
			return
		default:
		}

		log.Printf("[MARKET] ▶️ WS connect %s %d symbols", tf, len(watch))
		ch := c.StreamCandlesBatch(ctx, watch, tf)

		for {
			select {
			case <-ctx.Done():
				return
			case tick, ok := <-ch:
				if !ok {
					log.Printf("[MARKET] [%s] WS закрыт, переподключаемся", tf)
					time.Sleep(time.Second)
					goto reconnect
				}
				out <- OutTick{
					InstID:    tick.InstID,
					Timeframe: tf,
					Candle: models.CandleTick{
						Open:   tick.Open,
						High:   tick.High,
						Low:    tick.Low,
						Close:  tick.Close,
						Volume: tick.Volume,
					},
				}
			}
		}

	reconnect:
		c.n.SendService(ctx, "⚠️ MARKET[%s]: reconnect…", tf)
		time.Sleep(time.Second)
	}
}
