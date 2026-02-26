package service

import (
	"context"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/modules/config"
	okxws "trade_bot/internal/modules/okx_websocket/service"

	"trade_bot/internal/models"
)

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

type Service struct {
	cfg *config.Config

	// выходы (бывшие hub.out / hub.candleOut)
	out       chan<- models.Signal
	candleOut chan<- models.CandleTick

	// warmup флаг (ставит Warmuper)
	warmupDone atomic.Bool

	mu sync.Mutex
	st map[string]*v2State

	rejects *RejectStats

	tuneMu sync.RWMutex
	tune   RuntimeTuning

	lastSignalAt time.Time
	lastTuneAt   time.Time
}

func NewService(cfg *config.Config, out chan<- models.Signal, candleOut chan<- models.CandleTick) *Service {
	return &Service{
		cfg:       cfg,
		out:       out,
		candleOut: candleOut,
		st:        make(map[string]*v2State),
		rejects:   NewRejectStats(),
		tune: RuntimeTuning{
			MinChannelPct: cfg.Strategy.MinChannelPct,
			MinBodyPct:    cfg.Strategy.MinBodyPct,
			BreakoutPct:   cfg.Strategy.BreakoutPct,
			CloseUpMin:    0.80,
			CloseDnMax:    0.20,
		},
	}
}

// OnTick ...
func (e *Service) OnTick(ctx context.Context, t okxws.OutTick) {
	// 1) проброс свечей 1m наружу (не блокируем)
	if helper.NormTF(t.Timeframe) == "1m" && e.candleOut != nil {
		select {
		case e.candleOut <- t.Candle:
		default:
		}
	}

	// 2) стратегия
	sig, ok, _ := e.OnCandle(t.Candle)

	// 3) блок сигналов пока warmup не done
	if !ok || !e.warmupDone.Load() || e.out == nil {
		return
	}

	// 4) сигнал наружу (не блокируем)
	select {
	case e.out <- sig:
	default:
	}
}

func (e *Service) SetWarmupDone() {
	e.warmupDone.Store(true)
	log.Printf("[STRAT] warmup marked as done")
}

func (e *Service) IsWarmupDone() bool { return e.warmupDone.Load() }

func (e *Service) MaybeLogRejects() {
	// не чаще раза в минуту
	if !e.rejects.lastRejectLog.IsZero() && time.Since(e.rejects.lastRejectLog) < time.Minute {
		return
	}

	snap := e.rejects.Snapshot(false)
	if snap.Total == 0 {
		e.rejects.lastRejectLog = time.Now()
		return
	}

	// логируем топ-3
	n := 3
	if len(snap.Top) < n {
		n = len(snap.Top)
	}

	msg := ""
	for i := 0; i < n; i++ {
		msg += string(snap.Top[i].Reason) + "=" + itoaU64(snap.Top[i].Count) + " "
	}

	log.Printf("[STRAT] rejects: total=%d top=%s", snap.Total, msg)
	e.rejects.lastRejectLog = time.Now()
}

func (e *Service) MaybeAutoTuneNow() {
	if !e.IsWarmupDone() {
		return
	}

	now := time.Now()
	changed, before, after := e.MaybeAutoTune(now)
	if !changed {
		return
	}

	log.Printf("[TUNE] авто-ослабление: breakout %.4f->%.4f, ch %.4f->%.4f, body %.4f->%.4f, closeUp %.2f->%.2f, closeDn %.2f->%.2f",
		before.BreakoutPct, after.BreakoutPct,
		before.MinChannelPct, after.MinChannelPct,
		before.MinBodyPct, after.MinBodyPct,
		before.CloseUpMin, after.CloseUpMin,
		before.CloseDnMax, after.CloseDnMax,
	)
}
func (e *Service) get(sym string) *v2State {
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
func (e *Service) OnCandle(t models.CandleTick) (models.Signal, bool, bool) {
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
		e.rejects.Inc(RejectInvalidPrice)
		e.MaybeLogRejects()
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
			e.rejects.Inc(RejectNoChannel)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if !st.readyLTF || !st.readyHTF {
			e.rejects.Inc(RejectNotReady)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if st.trend == TrendNone {
			e.rejects.Inc(RejectNoTrend)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- ширина канала ----
		chPct = (dh - dl) / t.Close
		if chPct < minCh {
			e.rejects.Inc(RejectSmallChannel)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- тело свечи ----
		bodyPct = math.Abs(t.Close-t.Open) / t.Close
		if bodyPct < minBody {
			e.rejects.Inc(RejectSmallBody)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
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
			e.rejects.Inc(RejectZeroRange)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		closePos := (t.Close - t.Low) / rng

		if st.trend == TrendUp && closePos < closeUp {
			e.rejects.Inc(RejectWeakCloseUp)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if st.trend == TrendDown && closePos > closeDn {
			e.rejects.Inc(RejectWeakCloseDown)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		var side models.Side

		if st.trend == TrendUp && brokeUpByBody && upBoPct >= bo {
			side = models.SideBuy
		} else if st.trend == TrendDown && brokeDnByBody && dnBoPct >= bo {
			side = models.SideSell
		} else {
			e.rejects.Inc(RejectNoBreakout)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
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

		e.MaybeLogRejects()
		e.lastSignalAt = time.Now()

		return sig, true, becameReady

	default:
		return models.Signal{}, false, false
	}
}

func (e *Service) updateBuffer(st *v2State, t models.CandleTick) {
	st.highs = append(st.highs, t.High)
	st.lows = append(st.lows, t.Low)

	if len(st.highs) > e.cfg.Strategy.DonchianPeriod {
		st.highs = st.highs[1:]
		st.lows = st.lows[1:]
	}
}
func (e *Service) MaybeAutoTune(now time.Time) (changed bool, before RuntimeTuning, after RuntimeTuning) {
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

func (e *Service) RejectSnapshot(reset bool) RejectSnapshot {
	return e.rejects.Snapshot(reset)
}
func (e *Service) CurrentTuning() (t RuntimeTuning, lastSignalAt, lastTuneAt time.Time) {
	e.tuneMu.RLock()
	defer e.tuneMu.RUnlock()
	return e.tune, e.lastSignalAt, e.lastTuneAt
}
