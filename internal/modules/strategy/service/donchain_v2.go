package service

import (
	"fmt"
	"log"
	"math"
	"sync"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/modules/config"

	"trade_bot/internal/models"
)

type Trend int

const (
	TrendNone Trend = iota
	TrendUp
	TrendDown
)

type DonchianV2HTF struct {
	cfg *config.Config
	mu  sync.Mutex
	st  map[string]*v2State

	rejectMu    sync.Mutex
	rejectStats map[string]int
	lastLog     time.Time

	tuneMu sync.RWMutex
	tune   RuntimeTuning

	lastSignalAt time.Time
	lastTuneAt   time.Time
}
type RuntimeTuning struct {
	MinChannelPct float64
	MinBodyPct    float64
	BreakoutPct   float64
	CloseUpMin    float64 // было 0.80
	CloseDnMax    float64 // было 0.20
}
type v2State struct {
	// LTF
	highs    []float64
	lows     []float64
	wLTF     int
	readyLTF bool

	// HTF
	emaFast  emaState
	emaSlow  emaState
	wHTF     int
	readyHTF bool
	trend    Trend

	// anti-spam: одна LTF свеча -> максимум 1 сигнал
	lastSignalEnd time.Time
}

func NewDonchianV2HTF(cfg *config.Config) *DonchianV2HTF {

	return &DonchianV2HTF{
		cfg:         cfg,
		st:          make(map[string]*v2State),
		rejectStats: make(map[string]int),
		tune: RuntimeTuning{
			MinChannelPct: cfg.Strategy.MinChannelPct,
			MinBodyPct:    cfg.Strategy.MinBodyPct,
			BreakoutPct:   cfg.Strategy.BreakoutPct,
			CloseUpMin:    0.80,
			CloseDnMax:    0.20,
		},
	}
}
func (e *DonchianV2HTF) reject(reason string) {
	e.rejectMu.Lock()
	e.rejectStats[reason]++
	e.rejectMu.Unlock()
}
func (e *DonchianV2HTF) maybeLogRejects() {
	e.rejectMu.Lock()
	defer e.rejectMu.Unlock()

	if time.Since(e.lastLog) < time.Minute {
		return
	}

	if len(e.rejectStats) == 0 {
		e.lastLog = time.Now()
		return
	}

	total := 0
	for _, v := range e.rejectStats {
		total += v
	}

	msg := "[STRAT] reject stats: "
	for k, v := range e.rejectStats {
		msg += fmt.Sprintf("%s=%d ", k, v)
	}

	log.Println(msg)

	// reset
	e.rejectStats = make(map[string]int)
	e.lastLog = time.Now()
}
func (e *DonchianV2HTF) get(sym string) *v2State {
	if s, ok := e.st[sym]; ok {
		return s
	}
	s := &v2State{
		highs:   make([]float64, 0, e.cfg.Strategy.DonchianPeriod),
		lows:    make([]float64, 0, e.cfg.Strategy.DonchianPeriod),
		emaFast: newEMA(e.cfg.Strategy.HTFEmaFast),
		emaSlow: newEMA(e.cfg.Strategy.HTFEmaSlow),
		trend:   TrendNone,
	}
	e.st[sym] = s
	return s
}

// OnCandle принимает закрытые свечи разных ТФ (LTF/HTF) и решает, есть ли сигнал.
// returns:
//
//	sig, ok=true  -> есть сигнал
//	becameReady=true -> по этому символу стратегия впервые "прогрелась" (LTF/HTF)
func (e *DonchianV2HTF) OnCandle(t models.CandleTick) (models.Signal, bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.tuneMu.RLock()
	minCh := e.tune.MinChannelPct
	minBody := e.tune.MinBodyPct
	bo := e.tune.BreakoutPct
	closeUp := e.tune.CloseUpMin
	closeDn := e.tune.CloseDnMax
	e.tuneMu.RUnlock()

	tf := helper.NormTF(t.TimeframeRaw)
	st := e.get(t.InstID)

	becameReady := false

	// ---- защита от мусора ----
	if t.Close <= 0 || t.High <= 0 || t.Low <= 0 {
		e.reject("invalid_price")
		e.maybeLogRejects()
		return models.Signal{}, false, false
	}

	switch tf {

	// =========================================================
	// ===================== HTF ===============================
	// =========================================================
	case helper.NormTF(e.cfg.Strategy.HTF):

		st.emaFast.Update(t.Close)
		st.emaSlow.Update(t.Close)
		st.wHTF++

		if st.wHTF >= e.cfg.Strategy.MinWarmupHTF &&
			st.emaFast.Ready() && st.emaSlow.Ready() {

			if !st.readyHTF {
				st.readyHTF = true
				becameReady = true
			}

			f := st.emaFast.Value()
			s := st.emaSlow.Value()

			switch {
			case f > s:
				st.trend = TrendUp
			case f < s:
				st.trend = TrendDown
			default:
				st.trend = TrendNone
			}
		}

		return models.Signal{}, false, becameReady

	// =========================================================
	// ===================== LTF ===============================
	// =========================================================
	case helper.NormTF(e.cfg.Strategy.LTF):

		var (
			dh, dl  float64
			haveCh  bool
			chPct   float64
			bodyPct float64
		)

		// ---- канал ДО добавления текущей свечи ----
		if len(st.highs) >= e.cfg.Strategy.DonchianPeriod {
			dh = maxSlice(st.highs)
			dl = minSlice(st.lows)
			if dh > 0 && dl > 0 && dh > dl {
				haveCh = true
			}
		}

		// ---- прогрев LTF ----
		st.wLTF++
		if st.wLTF >= e.cfg.Strategy.MinWarmupLTF &&
			len(st.highs) >= e.cfg.Strategy.DonchianPeriod &&
			!st.readyLTF {

			st.readyLTF = true
			becameReady = true
		}

		// === БАЗОВЫЕ ПРОВЕРКИ ===
		if !haveCh {
			e.reject("no_channel")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if !st.readyLTF || !st.readyHTF {
			e.reject("not_ready")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if st.trend == TrendNone {
			e.reject("no_trend")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- ширина канала ----
		chPct = (dh - dl) / t.Close
		if chPct < minCh {
			e.reject("small_channel")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- тело свечи ----
		bodyPct = math.Abs(t.Close-t.Open) / t.Close
		if bodyPct < minBody {
			e.reject("small_body")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- breakout ----

		if bo <= 0 {
			bo = 0.002
		}

		upBoPct := (t.Close - dh) / dh
		dnBoPct := (dl - t.Close) / dl

		brokeUpByBody := t.Open <= dh && t.Close > dh
		brokeDnByBody := t.Open >= dl && t.Close < dl

		rng := t.High - t.Low
		if rng <= 0 {
			e.reject("zero_range")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		closePos := (t.Close - t.Low) / rng

		if st.trend == TrendUp && closePos < closeUp {
			e.reject("weak_close_up")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if st.trend == TrendDown && closePos > closeDn {
			e.reject("weak_close_down")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		var side models.Side

		if st.trend == TrendUp && brokeUpByBody && upBoPct >= bo {
			side = models.SideBuy
		} else if st.trend == TrendDown && brokeDnByBody && dnBoPct >= bo {
			side = models.SideSell
		} else {
			e.reject("no_breakout")
			e.updateBuffer(st, t)
			e.maybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- СИГНАЛ ----
		st.lastSignalEnd = t.End

		sig := models.Signal{
			InstID:    t.InstID,
			TF:        helper.NormTF(e.cfg.Strategy.LTF),
			Side:      side,
			Price:     t.Close,
			Strategy:  "donchian_v2_htf",
			CreatedAt: time.Now(),
		}

		e.updateBuffer(st, t)

		e.maybeLogRejects()
		e.lastSignalAt = time.Now()

		return sig, true, becameReady

	default:
		return models.Signal{}, false, false
	}
}

func (e *DonchianV2HTF) IsReady(symbol string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.st[symbol]
	if !ok {
		return false
	}
	return st.readyLTF && st.readyHTF && st.trend != TrendNone
}

func (e *DonchianV2HTF) Name() string { return "donchian_v2_htf1h" }

func (e *DonchianV2HTF) Dump(symbol string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	st, ok := e.st[symbol]
	if !ok {
		return "v2: no state"
	}

	dh := maxSlice(st.highs)
	dl := minSlice(st.lows)

	return fmt.Sprintf(
		"v2[15m] w15=%d/%d ready15=%v dh=%.6f dl=%.6f | [1h] w1h=%d fast=%.6f slow=%.6f trend=%v ready1h=%v",
		st.wLTF, e.cfg.Strategy.MinWarmupLTF, st.readyLTF, dh, dl,
		st.wHTF, st.emaFast.Value(), st.emaSlow.Value(), st.trend, st.readyHTF,
	)
}
func (t Trend) String() string {
	switch t {
	case TrendUp:
		return "up"
	case TrendDown:
		return "down"
	default:
		return "none"
	}
}
func (e *DonchianV2HTF) updateBuffer(st *v2State, t models.CandleTick) {
	st.highs = append(st.highs, t.High)
	st.lows = append(st.lows, t.Low)

	if len(st.highs) > e.cfg.Strategy.DonchianPeriod {
		st.highs = st.highs[1:]
		st.lows = st.lows[1:]
	}
}
func (e *DonchianV2HTF) MaybeAutoTune(now time.Time) (changed bool, before RuntimeTuning, after RuntimeTuning) {
	// если ещё не было сигналов — считаем от старта
	if e.lastSignalAt.IsZero() {
		e.lastSignalAt = now
		return false, RuntimeTuning{}, RuntimeTuning{}
	}

	// если недавно уже тюнили — не трогаем
	if !e.lastTuneAt.IsZero() && now.Sub(e.lastTuneAt) < 30*time.Minute {
		return false, RuntimeTuning{}, RuntimeTuning{}
	}

	// если сигнал был недавно — не трогаем
	if now.Sub(e.lastSignalAt) < 60*time.Minute {
		return false, RuntimeTuning{}, RuntimeTuning{}
	}

	e.tuneMu.Lock()
	defer e.tuneMu.Unlock()

	before = e.tune
	after = e.tune

	// Ослабляем на 15% за шаг, но не ниже “пола”
	after.BreakoutPct = maxf(after.BreakoutPct*0.85, 0.0012)
	after.MinChannelPct = maxf(after.MinChannelPct*0.85, 0.004)
	after.MinBodyPct = maxf(after.MinBodyPct*0.85, 0.0015)

	// close near edge чуть мягче
	after.CloseUpMin = maxf(after.CloseUpMin-0.03, 0.65)
	after.CloseDnMax = minf(after.CloseDnMax+0.03, 0.35)

	// если ничего не поменялось — выходим
	if after == before {
		return false, RuntimeTuning{}, RuntimeTuning{}
	}

	e.tune = after
	e.lastTuneAt = now
	e.lastSignalAt = now // чтобы не тюнить каждую минуту

	return true, before, after
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
