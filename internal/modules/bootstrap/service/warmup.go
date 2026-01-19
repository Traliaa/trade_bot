package service

import (
	"context"
	"fmt"
	"sync"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okxws "trade_bot/internal/modules/okx_websocket/service"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/modules/telegram_bot/service"
)

type Warmuper struct {
	mx  *okxws.Client
	hub *strategy.Hub
	n   *service.Telegram

	cfg *config.Config

	// ограничитель параллелизма, чтобы не словить rate limit
	sem chan struct{}
}

func NewWarmuper(mx *okxws.Client, hub *strategy.Hub, n *service.Telegram, cfg *config.Config) *Warmuper {
	return &Warmuper{
		mx:  mx,
		hub: hub,
		n:   n,
		cfg: cfg,
		sem: make(chan struct{}, 8), // 8 параллельных символов
	}
}

func (w *Warmuper) Warmup(ctx context.Context, symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	ltfNeed := w.cfg.DonchianPeriod + 30
	htfNeed := w.cfg.HTFEmaSlow + 30

	w.n.SendService(ctx, fmt.Sprintf("🔥 REST warmup start: symbols=%d LTF=%s(%d) HTF=%s(%d)",
		len(symbols), w.cfg.LTF, ltfNeed, w.cfg.HTF, htfNeed,
	))
	var cnt int64
	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for _, sym := range symbols {
		sym := sym
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.sem <- struct{}{}
			defer func() { <-w.sem }()

			// 1) HTF
			htf, err := w.mx.GetCandles(ctx, sym, w.cfg.HTF, htfNeed)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("warmup HTF %s: %w", sym, err)
				}
				mu.Unlock()
				return
			}
			for _, c := range htf {
				cnt++
				w.hub.OnTick(ctx, okxws.OutTick{
					InstID:    sym,
					Timeframe: w.cfg.HTF,
					Candle:    models.CandleTick{Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Start: c.Start, End: c.End},
				})
			}

			// 2) LTF
			ltf, err := w.mx.GetCandles(ctx, sym, w.cfg.LTF, ltfNeed)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("warmup LTF %s: %w", sym, err)
				}
				mu.Unlock()
				return
			}
			for _, c := range ltf {
				cnt++
				w.hub.OnTick(ctx, okxws.OutTick{
					InstID:    sym,
					Timeframe: w.cfg.LTF,
					Candle:    models.CandleTick{Open: c.Open, High: c.High, Low: c.Low, Close: c.Close, Start: c.Start, End: c.End},
				})
			}
		}()
	}

	wg.Wait()

	if firstErr != nil {
		w.n.SendService(ctx, "⚠️ REST warmup finished with error: "+firstErr.Error())
		return firstErr
	}

	w.n.SendService(ctx, "✅ REST warmup finished. WS can start immediately.")
	return nil
}
