package service

import (
	"context"
	"sync"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/telegram_public/public"

	"trade_bot/internal/models"
	okxws "trade_bot/internal/modules/okx_websocket/service"
)

type ServiceNotifier interface {
	Set(ctx context.Context, st public.Status)
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
	actualSymbols int
	lastWarmupPct int
}

func NewHub(cfg *config.Config, n *public.Service, out chan<- models.Signal, candleOut chan<- models.CandleTick, engine Engine) *Hub {
	return &Hub{
		cfg:       cfg,
		n:         n,
		out:       out,
		engine:    engine,
		candleOut: candleOut,
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
	actual := h.actualSymbols
	expected = h.expectedSymbols(actual)
	if expected <= 0 {
		return
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

	// expected строго по твоему конфигу
	expected := h.cfg.Strategy.ExpectedSymbols
	if expected <= 0 {
		expected = h.cfg.Strategy.WatchTopN
	}
	if expected <= 0 {
		return
	}

	now := time.Now()

	// дефолт, чтобы не было спама если progress_every не задан
	every := h.cfg.Strategy.ProgressEvery
	if every <= 0 {
		every = 5 * time.Second
	}

	// 1) Обновление прогресса (не чаще чем every)
	if h.n != nil && (h.lastProgress.IsZero() || now.Sub(h.lastProgress) >= every) {
		pct := int(float64(h.readyCnt) * 100 / float64(expected))
		if pct < 0 {
			pct = 0
		}
		if pct > 99 {
			pct = 99
		}

	}

	// 2) Stall detector (без WarmupPct, только константа)
	if h.lastReadyAt.IsZero() {
		h.lastReadyAt = now
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

func (h *Hub) updateWarmupStatusLocked(ctx context.Context, expected int) {
	if h.n == nil {
		return
	}

	now := time.Now()

	// Не чаще чем ProgressEvery
	if !h.lastProgress.IsZero() && now.Sub(h.lastProgress) < h.cfg.Strategy.ProgressEvery {
		return
	}

	pct := int(float64(h.readyCnt) * 100 / float64(expected))
	if pct < 0 {
		pct = 0
	}
	if pct > 99 && !h.warmupDone {
		pct = 99
	}

	h.lastProgress = now

	h.n.Set(ctx, public.Status{
		State:       public.StatePreparing,
		Exchange:    "OKX",
		Instruments: expected,
		Progress:    pct, // прогресс-бар внутри Render()
	})
}

func (h *Hub) expectedSymbols(actual int) int {
	if h.cfg.Strategy.ExpectedSymbols > 0 {
		return h.cfg.Strategy.ExpectedSymbols
	}
	if actual > 0 {
		return actual
	}
	if h.cfg.Strategy.WatchTopN > 0 {
		return h.cfg.Strategy.WatchTopN
	}
	return 0
}
