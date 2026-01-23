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

	lastReadyCnt  int
	lastReadyAt   time.Time
	warmupStarted time.Time
	warmupStalled bool
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

	if becameReady {
		h.onBecameReady(ctx, ct.InstID)

		// прогресс обновился
		h.mu.Lock()
		h.lastReadyCnt = h.readyCnt
		h.lastReadyAt = time.Now()
		h.mu.Unlock()
	} else {
		h.maybeWarmupProgress(ctx)
	}

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

	if h.ready == nil {
		h.ready = make(map[string]bool)
	}

	if h.ready[sym] {
		return
	}

	h.ready[sym] = true
	h.readyCnt++
	h.lastReadyAt = time.Now() // ✅ прогресс реально сдвинулся

	expected := h.cfg.Strategy.WatchTopN

	// старт (один раз)
	if !h.warmupMsgSent {
		h.warmupMsgSent = true
		h.lastProgress = time.Now()
		if h.n != nil {
			h.n.SendService(ctx,
				"🔥 Warmup started | engine=%s | LTF=%s HTF=%s | ожидаем=%d",
				h.engine.Name(), h.cfg.Strategy.LTF, h.cfg.Strategy.HTF, expected,
			)
		}
		// не return — пусть может сразу завершиться, если expected маленький
	}

	// done
	if !h.warmupDone && expected > 0 && h.readyCnt >= expected {
		h.warmupDone = true
		if h.n != nil {
			h.n.SendService(ctx,
				"✅ Warmup finished: %d/%d ready. Теперь ждём сигналы.",
				h.readyCnt, expected,
			)
		}
	}
}

func (h *Hub) isWarmupDone() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.warmupDone
}

func (h *Hub) maybeWarmupProgress(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.warmupDone {
		return
	}

	now := time.Now()

	// Инициализация таймеров
	if h.startedAt.IsZero() {
		h.startedAt = now
	}
	if h.lastReadyAt.IsZero() {
		h.lastReadyAt = now
	}

	expected := h.cfg.Strategy.WatchTopN
	if expected <= 0 {
		return
	}

	// 1) прогресс-лог не чаще чем ProgressEvery
	if !h.lastProgress.IsZero() && now.Sub(h.lastProgress) >= h.cfg.Strategy.ProgressEvery {
		if h.n != nil {
			h.n.SendService(ctx, "⏳ Warmup progress: %d/%d ready", h.readyCnt, expected)
		}
		h.lastProgress = now
	}

	// 2) stall-detector: если почти всё готово и давно нет прогресса — считаем done
	stallTimeout := 5 * time.Minute
	minRatio := 0.99 // можно 0.95, если хочешь агрессивнее
	minReady := int(float64(expected) * minRatio)

	if h.readyCnt >= minReady && now.Sub(h.lastReadyAt) >= stallTimeout {
		h.warmupDone = true

		var miss []string
		for inst, ok := range h.ready {
			if !ok {
				miss = append(miss, inst)
				if len(miss) >= 5 {
					break
				}
			}
		}

		if h.n != nil {
			h.n.SendService(ctx,
				"⚠️ Warmup stalled: %d/%d ready for %s. Continue without: %v",
				h.readyCnt, expected, stallTimeout, miss,
			)
		}
	}
}

func (h *Hub) notReadySymbols(limit int) []string {
	out := make([]string, 0, limit)
	for inst, ok := range h.ready {
		if !ok {
			out = append(out, inst)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
