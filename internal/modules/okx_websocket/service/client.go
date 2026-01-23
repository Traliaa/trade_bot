package service

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
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
		apiKey:    cfg.OKXWS.APIKey,
		apiSecret: cfg.OKXWS.APISecret,
		passph:    cfg.OKXWS.Passphrase,
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
// Start собирает топ-волатильные и стримит по нескольким таймфреймам.
func (c *Client) Start(ctx context.Context, out chan<- OutTick) {
	syms := c.TopVolatile(c.cfg.Strategy.WatchTopN)
	if len(syms) == 0 {
		if c.n != nil {
			c.n.SendService(ctx, "⚠️ *Рынок:* не удалось собрать список волатильных инструментов — стример не запущен.")
		}
		log.Println("[MARKET] пустой список волатильных инструментов")
		return
	}

	timeframes := []string{"1m", "5m", "15m"}

	if c.n != nil {
		c.n.SendService(ctx, fmt.Sprintf(
			"🚀 OKX: WebSocket-стример запущен\n"+
				"• Таймфреймы: 1m / 5m / 15m\n"+
				"• Инструментов: 100",
			strings.Join(timeframes, " / "),
			len(syms),
		))
	}

	for _, tf := range timeframes {
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
		c.n.SendService(ctx, fmt.Sprintf(
			"[РЫНОК] ▶️ WS: подключение %s — инструментов: %d",
			timeframe, len(syms),
		))
	}

	ticks := c.StreamCandlesBatch(ctx, syms, timeframe)

	for {
		select {
		case <-ctx.Done():
			if c.n != nil {
				c.n.SendService(ctx, fmt.Sprintf(
					"[РЫНОК] ⏹ WS: остановка %s",
					timeframe,
				))
			}
			return

		case tick, ok := <-ticks:
			if !ok {
				if c.n != nil {
					c.n.SendService(ctx, fmt.Sprintf(
						"[РЫНОК] ❌ WS: поток закрыт %s",
						timeframe,
					))
				}
				return
			}

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
