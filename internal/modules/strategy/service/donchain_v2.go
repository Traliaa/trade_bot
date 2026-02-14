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

		// ---- базовые условия готовности ----
		if !haveCh {
			e.reject("no_channel")
			goto UPDATE
		}
		if !st.readyLTF || !st.readyHTF {
			e.reject("not_ready")
			goto UPDATE
		}
		if st.trend == TrendNone {
			e.reject("no_trend")
			goto UPDATE
		}

		// ---- ширина канала ----
		chPct = (dh - dl) / t.Close
		if chPct < e.cfg.Strategy.MinChannelPct {
			e.reject("small_channel")
			goto UPDATE
		}

		// ---- тело свечи ----
		bodyPct = math.Abs(t.Close-t.Open) / t.Close
		if bodyPct < e.cfg.Strategy.MinBodyPct {
			e.reject("small_body")
			goto UPDATE
		}

		// ---- breakout ----
		bo := e.cfg.Strategy.BreakoutPct
		if bo < 0 {
			bo = 0
		}
		if bo == 0 {
			bo = 0.002
		}

		upBoPct := (t.Close - dh) / dh
		dnBoPct := (dl - t.Close) / dl

		brokeUpByBody := t.Open <= dh && t.Close > dh
		brokeDnByBody := t.Open >= dl && t.Close < dl

		// ---- close near edge фильтр ----
		rng := t.High - t.Low
		if rng <= 0 {
			e.reject("zero_range")
			goto UPDATE
		}

		closePos := (t.Close - t.Low) / rng

		if st.trend == TrendUp && closePos < 0.80 {
			e.reject("weak_close_up")
			goto UPDATE
		}
		if st.trend == TrendDown && closePos > 0.20 {
			e.reject("weak_close_down")
			goto UPDATE
		}

		var side models.Side

		switch {
		case st.trend == TrendUp && brokeUpByBody && upBoPct >= bo:
			side = models.SideBuy

		case st.trend == TrendDown && brokeDnByBody && dnBoPct >= bo:
			side = models.SideSell

		default:
			e.reject("no_breakout")
			goto UPDATE
		}

		// ---- сигнал ----
		st.lastSignalEnd = t.End

		sig := models.Signal{
			InstID:   t.InstID,
			TF:       helper.NormTF(e.cfg.Strategy.LTF),
			Side:     side,
			Price:    t.Close,
			Strategy: "donchian_v2_htf",
			Reason: fmt.Sprintf(
				"trend=%v Don[%d] chPct=%.4f bodyPct=%.4f bo=%.4f upBo=%.4f dnBo=%.4f dh=%.6f dl=%.6f",
				st.trend, e.cfg.Strategy.DonchianPeriod,
				chPct, bodyPct, bo, upBoPct, dnBoPct, dh, dl,
			),
			CreatedAt: time.Now(),
		}

		st.highs = append(st.highs, t.High)
		st.lows = append(st.lows, t.Low)
		if len(st.highs) > e.cfg.Strategy.DonchianPeriod {
			st.highs = st.highs[1:]
			st.lows = st.lows[1:]
		}

		fmt.Printf("[SIG] %s %s close=%.6f dh=%.6f dl=%.6f trend=%v upBo=%.4f dnBo=%.4f\n",
			t.InstID, side, t.Close, dh, dl, st.trend, upBoPct, dnBoPct)

		e.maybeLogRejects()

		return sig, true, becameReady

	UPDATE:
		st.highs = append(st.highs, t.High)
		st.lows = append(st.lows, t.Low)
		if len(st.highs) > e.cfg.Strategy.DonchianPeriod {
			st.highs = st.highs[1:]
			st.lows = st.lows[1:]
		}

		e.maybeLogRejects()
		return models.Signal{}, false, becameReady

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
