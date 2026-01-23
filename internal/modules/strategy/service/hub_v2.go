package service

import (
	"context"
	"fmt"
	"sync"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/modules/config"

	"trade_bot/internal/models"
	okxws "trade_bot/internal/modules/okx_websocket/service"
)

type ServiceNotifier interface {
	SendService(ctx context.Context, format string, args ...any)
}

type Hub struct {
	cfg       *config.Config
	n         ServiceNotifier
	out       chan<- models.Signal
	candleOut chan<- models.CandleTick

	engine Engine

	mu            sync.Mutex
	readyCnt      int
	ready         map[string]bool
	warmupDone    bool
	warmupMsgSent bool
	lastProgress  time.Time
	startedAt     time.Time
}

func NewHub(cfg *config.Config, n ServiceNotifier, out chan<- models.Signal, engine Engine) *Hub {
	return &Hub{
		cfg:       cfg,
		n:         n,
		out:       out,
		engine:    engine,
		ready:     make(map[string]bool),
		startedAt: time.Now(),
	}
}

func (h *Hub) OnTick(ctx context.Context, t okxws.OutTick) {
	// приводим WS tick к models.CandleTick
	ct := models.CandleTick{
		InstID:       t.InstID,
		Open:         t.Candle.Open,
		High:         t.Candle.High,
		Low:          t.Candle.Low,
		Close:        t.Candle.Close,
		Volume:       t.Candle.Volume,
		QuoteVolume:  t.Candle.QuoteVolume,
		Start:        t.Candle.Start,
		End:          t.Candle.End,
		TimeframeRaw: t.Timeframe,
	}

	sig, ok, becameReady := h.engine.OnCandle(ct)

	// прогресс прогрева
	if becameReady {
		h.onBecameReady(ctx, ct.InstID)
	} else {
		h.maybeWarmupProgress(ctx)
	}

	if helper.NormTF(ct.TimeframeRaw) == "1m" {
		select {
		case h.candleOut <- ct:
			fmt.Printf("[CANDLE OUT] %s 1m close=%.6f end=%s\n", ct.InstID, ct.Close, ct.End.Format(time.RFC3339))

		default:
		}
	}

	// блокируем сигналы пока прогрев не окончен
	if !ok || !h.isWarmupDone() {
		return
	}

	// отдаём сигнал наружу (лучше не блокировать Hub)
	select {
	case h.out <- sig:
	default:
		if h.n != nil {
			h.n.SendService(ctx, "⚠️ signal channel full, drop %s %s @ %.6f (%s)",
				sig.InstID, sig.Side, sig.Price, sig.TF)
		}
	}
}

func (h *Hub) onBecameReady(ctx context.Context, sym string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.ready[sym] {
		return
	}
	h.ready[sym] = true
	h.readyCnt++

	// старт (один раз)
	if !h.warmupMsgSent {
		h.warmupMsgSent = true
		h.lastProgress = time.Now()
		if h.n != nil {
			h.n.SendService(ctx,
				"🔥 Warmup started | engine=%s | LTF=%s HTF=%s | ожидаем=%d",
				h.engine.Name(), h.cfg.Strategy.LTF, h.cfg.Strategy.HTF, h.cfg.Strategy.WatchTopN,
			)
		}
		return
	}

	// done
	if !h.warmupDone && h.readyCnt >= h.cfg.Strategy.WatchTopN {
		h.warmupDone = true
		if h.n != nil {
			h.n.SendService(ctx,
				"✅ Warmup finished: %d/%d ready. Теперь ждём сигналы.",
				h.readyCnt, h.cfg.Strategy.WatchTopN,
			)
		}
	}
}

func (h *Hub) maybeWarmupProgress(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.warmupMsgSent || h.warmupDone || h.n == nil {
		return
	}
	if h.cfg.Strategy.ProgressEvery <= 0 {
		return
	}
	if time.Since(h.lastProgress) < h.cfg.Strategy.ProgressEvery {
		return
	}

	h.n.SendService(ctx, "⏳ Warmup progress: %d/%d ready", h.readyCnt, h.cfg.Strategy.WatchTopN)
	h.lastProgress = time.Now()
}

func (h *Hub) isWarmupDone() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.warmupDone
}
