package service

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/telegram_public/public"

	"github.com/gorilla/websocket"
)

type ServiceNotifier interface {
	SendServiceText(ctx context.Context, text string) (messageID int, err error)
	EditServiceText(ctx context.Context, messageID int, text string) error
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

// Start собирает топ-волатильные и стримит по нескольким таймфреймам.
func (c *Client) Start(ctx context.Context, out chan<- models.CandleTick) {
	syms, err := c.TopVolatile(c.cfg.Strategy.WatchTopN)
	if err != nil {
		log.Printf("[MARKET] ошибка TopVolatile: %v", err)
		return
	}
	if len(syms) == 0 {
		log.Println("[MARKET] пустой список волатильных инструментов")
		return
	}

	timeframes := uniqTimeframes("1m", c.cfg.Strategy.LTF, c.cfg.Strategy.HTF)

	if c.n != nil {
		_, _ = c.n.SendServiceText(ctx, public.Status{
			State:       public.StateRestarting,
			Exchange:    "OKX",
			Instruments: len(syms),
			UpdatedAt:   time.Now(),
		}.RenderHTML())
	}

	for _, tf := range timeframes {
		okxBar := toOKXBar(tf)
		go c.runTimeframe(ctx, tf, okxBar, syms, out)
	}
}

func (c *Client) runTimeframe(
	ctx context.Context,
	internalTF string, // "1h"
	okxBar string, // "1H"
	syms []string,
	out chan<- models.CandleTick,
) {
	ticks := c.StreamCandlesBatch(ctx, syms, okxBar) // <-- ВАЖНО: okxBar

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

			select {
			case out <- candle:
			case <-ctx.Done():
				return
			}
		}
	}
}

func uniqTimeframes(tfs ...string) []string {
	m := make(map[string]struct{}, len(tfs))
	out := make([]string, 0, len(tfs))
	for _, tf := range tfs {
		tf = strings.TrimSpace(tf)
		if tf == "" {
			continue
		}
		if _, ok := m[tf]; ok {
			continue
		}
		m[tf] = struct{}{}
		out = append(out, tf)
	}
	return out
}

func toOKXBar(tf string) string {
	switch tf {
	case "1m", "3m", "5m", "15m", "30m":
		return tf
	case "1h":
		return "1H"
	case "2h":
		return "2H"
	case "4h":
		return "4H"
	case "6h":
		return "6H"
	case "12h":
		return "12H"
	case "1d":
		return "1D"
	case "1w":
		return "1W"
	default:
		return tf // fallback, но лучше логнуть
	}
}

func okxCandleChannel(okxBar string) string {
	return "candle" + okxBar
}
