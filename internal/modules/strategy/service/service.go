package service

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"

	"go.uber.org/zap"
)

type Service struct {
	base.Base

	cfg *config.Config

	// выходы (бывшие hub.out / hub.candleOut)
	out       chan<- models.Signal
	candleOut chan<- models.CandleTick

	// warmup флаг (ставит Warmuper)
	warmupDone atomic.Bool

	mu sync.Mutex
	st map[string]*models.V2State

	rejects    *models.RejectStats
	lastTickAt time.Time

	lastSignalAt time.Time

	tuneMu          sync.RWMutex
	tune            models.RuntimeTuning
	tuneModeMu      sync.RWMutex
	tuneMode        models.TuneMode
	lastTuneCheckAt time.Time
	lastTuneAt      time.Time
}

func NewService(cfg *config.Config, out chan<- models.Signal, candleOut chan<- models.CandleTick) *Service {
	s := &Service{
		cfg:       cfg,
		out:       out,
		candleOut: candleOut,
		st:        make(map[string]*models.V2State),
		rejects:   models.NewRejectStats(),
		tune: models.RuntimeTuning{
			MinChannelPct: cfg.Strategy.MinChannelPct,
			MinBodyPct:    cfg.Strategy.MinBodyPct,
			BreakoutPct:   cfg.Strategy.BreakoutPct,
			CloseUpMin:    0.70,
			CloseDnMax:    0.30,
		},
		tuneMode: models.TuneMode(cfg.Strategy.TuneMode),
	}

	// полезные поля один раз
	if s.Logger != nil {
		s.Logger = s.Logger.With(
			zap.String("strategy_ltf", helper.NormTF(cfg.Strategy.LTF)),
			zap.String("strategy_htf", helper.NormTF(cfg.Strategy.HTF)),
			zap.Int("donchian_period", cfg.Strategy.DonchianPeriod),
		)
	}

	return s
}

// Start ...
func (s *Service) Start(ctx context.Context, ticks <-chan models.CandleTick) error {
	ctx, shouldStart, started, stopped := s.StartInit(ctx)
	if !shouldStart {
		return nil
	}

	go func() {
		started()
		defer stopped()

		s.Logger.Info("strategy loop started")
		defer s.Logger.Info("strategy loop stopped", zap.Error(context.Cause(ctx)))

		for {
			select {
			case <-ctx.Done():
				return

			case t, ok := <-ticks:
				if !ok {
					s.Logger.Warn("ticks channel closed")
					return
				}

				// sample-log что тик реально дошел до стратегии
				if rand.Intn(2000) == 0 {
					s.Logger.Debug("tick recv (sample)",
						zap.String("instId", t.InstID),
						zap.String("tf", helper.NormTF(t.TimeframeRaw)),
						zap.Time("start", t.Start),
						zap.Float64("close", t.Close),
					)
				}

				s.OnTick(ctx, t)
			}
		}
	}()

	return nil
}

// OnTick ...
func (e *Service) OnTick(ctx context.Context, t models.CandleTick) {
	e.rejects.Touch(time.Now())

	tf := helper.NormTF(t.TimeframeRaw)

	// 1) проброс свечей 1m наружу (не блокируем)
	if tf == "1m" && e.candleOut != nil {
		select {
		case e.candleOut <- t:
			// sample, иначе спам
			if rand.Intn(2000) == 0 {
				e.Logger.Debug("candle forwarded (sample)",
					zap.String("instId", t.InstID),
					zap.Int("out_len", len(e.candleOut)), // если это chan<- нельзя len; см. ниже
					zap.Time("start", t.Start),
					zap.Float64("close", t.Close),
				)
			}
		default:
			// важно знать, что теряем 1m для трейла
			e.Logger.Warn("candleOut full, drop candle",
				zap.String("instId", t.InstID),
				zap.Time("start", t.Start),
				zap.Float64("close", t.Close),
			)
		}
		return
	}

	if tf == helper.NormTF(e.cfg.Strategy.LTF) {
		_ = e.MaybeAutoTuneTick(time.Now())
	}

	// 2) стратегия
	sig, ok, _ := e.OnCandle(t)

	// 3) блок сигналов пока warmup не done
	if !ok || !e.warmupDone.Load() || e.out == nil {
		return
	}

	// 4) сигнал наружу (не блокируем)
	select {
	case e.out <- sig:
		// sample
		if rand.Intn(200) == 0 {
			e.Logger.Info("signal sent (sample)",
				zap.String("instId", sig.InstID),
				zap.String("side", string(sig.Side)),
				zap.Float64("price", sig.Price),
				zap.String("tf", sig.TF),
			)
		}
	default:
		e.Logger.Warn("out channel full, drop signal",
			zap.String("instId", sig.InstID),
			zap.String("side", string(sig.Side)),
			zap.Float64("price", sig.Price),
			zap.String("tf", sig.TF),
		)
	}
}

func (e *Service) SetWarmupDone() {
	e.warmupDone.Store(true)
	e.Logger.Info("warmup marked as done")
}

func (e *Service) IsWarmupDone() bool { return e.warmupDone.Load() }

func (e *Service) MaybeLogRejects() {
	if !e.rejects.LastRejectLog.IsZero() && time.Since(e.rejects.LastRejectLog) < time.Minute {
		return
	}

	snap := e.rejects.Snapshot(false)
	e.rejects.LastRejectLog = time.Now()

	if snap.Total == 0 {
		return
	}

	n := 3
	if len(snap.Top) < n {
		n = len(snap.Top)
	}

	fields := make([]zap.Field, 0, 2+n*2)
	fields = append(fields, zap.Uint64("total", snap.Total))
	for i := 0; i < n; i++ {
		fields = append(fields,
			zap.String("r"+itoa(i+1), string(snap.Top[i].Reason)),
			zap.Uint64("c"+itoa(i+1), snap.Top[i].Count),
		)
	}

	e.Logger.Info("rejects snapshot", fields...)
}

func (e *Service) get(sym string) *models.V2State {
	if s, ok := e.st[sym]; ok {
		return s
	}
	s := &models.V2State{
		Highs:   make([]float64, 0, e.cfg.Strategy.DonchianPeriod),
		Lows:    make([]float64, 0, e.cfg.Strategy.DonchianPeriod),
		EmaFast: models.NewEMA(e.cfg.Strategy.HTFEmaFast),
		EmaSlow: models.NewEMA(e.cfg.Strategy.HTFEmaSlow),
		Trend:   models.TrendNone,
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
	e.lastTickAt = time.Now()
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
		e.rejects.Inc(models.RejectInvalidPrice)
		e.MaybeLogRejects()
		return models.Signal{}, false, false
	}

	switch tf {

	// =========================================================
	// ===================== HTF ===============================
	// =========================================================
	case helper.NormTF(e.cfg.Strategy.HTF):

		st.EmaFast.Update(t.Close)
		st.EmaSlow.Update(t.Close)
		st.WHTF++

		if st.WHTF >= e.cfg.Strategy.MinWarmupHTF &&
			st.EmaFast.Ready() && st.EmaSlow.Ready() {

			if !st.ReadyHTF {
				st.ReadyHTF = true
				becameReady = true
			}

			f := st.EmaFast.Value()
			s := st.EmaSlow.Value()

			switch {
			case f > s:
				st.Trend = models.TrendUp
			case f < s:
				st.Trend = models.TrendDown
			default:
				st.Trend = models.TrendNone
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
		if len(st.Highs) >= e.cfg.Strategy.DonchianPeriod {
			dh = maxSlice(st.Highs)
			dl = minSlice(st.Lows)
			if dh > 0 && dl > 0 && dh > dl {
				haveCh = true
			}
		}

		// ---- прогрев LTF ----
		st.WLTF++
		if st.WLTF >= e.cfg.Strategy.MinWarmupLTF &&
			len(st.Highs) >= e.cfg.Strategy.DonchianPeriod &&
			!st.ReadyLTF {

			st.ReadyLTF = true
			becameReady = true
		}

		// === БАЗОВЫЕ ПРОВЕРКИ ===
		if !haveCh {

			e.updateBuffer(st, t)
			return models.Signal{}, false, becameReady
		}

		if !st.ReadyLTF || !st.ReadyHTF {
			e.rejects.Inc(models.RejectNotReady)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if st.Trend == models.TrendNone {
			e.rejects.Inc(models.RejectNoTrend)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- ширина канала ----
		chPct = (dh - dl) / t.Close
		if chPct < minCh {
			e.rejects.Inc(models.RejectSmallChannel)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- тело свечи ----
		bodyPct = math.Abs(t.Close-t.Open) / t.Close
		if bodyPct < minBody {
			e.rejects.Inc(models.RejectSmallBody)
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
			e.rejects.Inc(models.RejectZeroRange)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		closePos := (t.Close - t.Low) / rng

		if st.Trend == models.TrendUp && closePos < closeUp {
			e.rejects.IncWeakClose(models.RejectWeakCloseUp, closePos)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		if st.Trend == models.TrendDown && closePos > closeDn {
			e.rejects.IncWeakClose(models.RejectWeakCloseDown, closePos)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}
		var side models.Side

		if st.Trend == models.TrendUp && brokeUpByBody && upBoPct >= bo {
			side = models.SideBuy
		} else if st.Trend == models.TrendDown && brokeDnByBody && dnBoPct >= bo {
			side = models.SideSell
		} else {
			e.rejects.Inc(models.RejectNoBreakout)
			e.updateBuffer(st, t)
			e.MaybeLogRejects()
			return models.Signal{}, false, becameReady
		}

		// ---- СИГНАЛ ----
		st.LastSignalEnd = t.End

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
		e.tuneMu.Lock()
		e.lastSignalAt = time.Now()
		e.tuneMu.Unlock()
		e.Logger.Warn("signal",
			zap.String("raw", t.TimeframeRaw),
			zap.String("norm", tf),
			zap.String("instId", t.InstID),
			zap.String("want_ltf", helper.NormTF(e.cfg.Strategy.LTF)),
			zap.String("want_htf", helper.NormTF(e.cfg.Strategy.HTF)),
		)
		return sig, true, becameReady

	default:
		e.rejects.Inc(models.RejectInternal)
		e.Logger.Warn("unknown timeframe",
			zap.String("raw", t.TimeframeRaw),
			zap.String("norm", tf),
			zap.String("instId", t.InstID),
			zap.String("want_ltf", helper.NormTF(e.cfg.Strategy.LTF)),
			zap.String("want_htf", helper.NormTF(e.cfg.Strategy.HTF)),
		)
		return models.Signal{}, false, false
	}
}

func (e *Service) updateBuffer(st *models.V2State, t models.CandleTick) {
	st.Highs = append(st.Highs, t.High)
	st.Lows = append(st.Lows, t.Low)

	if len(st.Highs) > e.cfg.Strategy.DonchianPeriod {
		st.Highs = st.Highs[1:]
		st.Lows = st.Lows[1:]
	}
}

func (e *Service) RejectSnapshot(reset bool) models.RejectSnapshot {
	return e.rejects.Snapshot(reset)
}
func (e *Service) CurrentTuning() (t models.RuntimeTuning, lastSignalAt, lastTuneAt time.Time) {
	e.tuneMu.RLock()
	defer e.tuneMu.RUnlock()
	return e.tune, e.lastSignalAt, e.lastTuneAt
}

func (e *Service) MaybeAutoTuneTick(now time.Time) models.TuneDecision {
	// не чаще чем раз в 5 минут
	if !e.lastTuneCheckAt.IsZero() && now.Sub(e.lastTuneCheckAt) < 5*time.Minute {
		return models.TuneDecision{Changed: false, Why: models.TuneWhyCooldown}
	}
	e.lastTuneCheckAt = now
	return e.MaybeAutoTuneAdaptive(now, false)
}
func (e *Service) MaybeAutoTuneAdaptive(now time.Time, force bool) models.TuneDecision {
	e.tuneModeMu.RLock()
	mode := e.tuneMode
	e.tuneModeMu.RUnlock()

	if mode == models.TuneOff {
		return models.TuneDecision{Changed: false, Why: models.TuneWhyOff}
	}

	// warmup — только для auto/safe, manual(force) пропускает
	if !force && !e.IsWarmupDone() {
		return models.TuneDecision{Changed: false, Why: models.TuneWhyWarmup}
	}

	// ❗️если сигналов ещё не было — auto/safe не тюним вообще
	// (и главное: не подменяем lastSignalAt)
	if !force {
		e.tuneMu.RLock()
		hasSignal := !e.lastSignalAt.IsZero()
		e.tuneMu.RUnlock()

		if !hasSignal {
			cur, _, _ := e.CurrentTuning()
			return models.TuneDecision{
				Changed: false,
				Why:     models.TuneWhyNoSignalsYet,
				Before:  cur,
				After:   cur,
				Total:   0,
				From:    now,
				To:      now,
			}
		}
	}

	// cooldown между тюнами (manual пропускает)
	cooldown := 30 * time.Minute
	if mode == models.TuneSafe {
		cooldown = 45 * time.Minute
	}
	if !force && !e.lastTuneAt.IsZero() && now.Sub(e.lastTuneAt) < cooldown {
		return models.TuneDecision{Changed: false, Why: models.TuneWhyCooldown}
	}

	// если сигнал был недавно — не тюним (manual пропускает)
	noSignalFor := 60 * time.Minute
	if mode == models.TuneAuto {
		noSignalFor = 30 * time.Minute
	}

	if !force {
		e.tuneMu.RLock()
		lastSig := e.lastSignalAt
		e.tuneMu.RUnlock()

		if !lastSig.IsZero() && now.Sub(lastSig) < noSignalFor {
			cur, _, _ := e.CurrentTuning()
			return models.TuneDecision{
				Changed: false,
				Why:     models.TuneWhySignalsRecent,
				Before:  cur,
				After:   cur,
				From:    now.Add(-noSignalFor),
				To:      now,
				Total:   0,
			}
		}
	}

	// 4) Берём снимок reject-окна
	snap := e.rejects.Snapshot(false)

	minRejects := uint64(30)
	if mode == models.TuneAuto {
		minRejects = 20
	}
	if snap.Total < minRejects {
		return models.TuneDecision{
			Changed: false, Why: models.TuneWhyNotEnoughData,
			Total: snap.Total, From: snap.From, To: snap.To,
		}
	}

	// 5) Доминирующая причина
	dom, domPct, total := dominantReason(snap)
	domMin := 0.50
	if mode == models.TuneAuto {
		domMin = 0.30
	}
	if dom == "" || domPct < domMin {
		return models.TuneDecision{
			Changed:  false,
			Why:      models.TuneWhyNoDominant,
			Dominant: dom,
			DomPct:   domPct,
			Total:    total,
			From:     snap.From,
			To:       snap.To,
		}
	}

	// 6) Применяем “селективный” тюн
	stepClose := 0.03
	mulSoft := 0.85
	if mode == models.TuneAuto {
		stepClose = 0.05
		mulSoft = 0.80
	}

	const (
		minCloseUp = 0.65
		maxCloseUp = 0.90
		minCloseDn = 0.10
		maxCloseDn = 0.35
		minBody    = 0.0010
		maxBody    = 0.0200
		minCh      = 0.0010
		maxCh      = 0.0500
		minBo      = 0.0008
		maxBo      = 0.0200
	)

	e.tuneMu.Lock()
	before := e.tune
	after := e.tune

	switch dom {
	case models.RejectWeakClose:
		after.CloseUpMin = clamp(after.CloseUpMin-stepClose, minCloseUp, maxCloseUp)
		after.CloseDnMax = clamp(after.CloseDnMax+stepClose, minCloseDn, maxCloseDn)
	case models.RejectSmallBody:
		after.MinBodyPct = clamp(after.MinBodyPct*mulSoft, minBody, maxBody)
	case models.RejectSmallChannel:
		after.MinChannelPct = clamp(after.MinChannelPct*mulSoft, minCh, maxCh)
	case models.RejectNoBreakout:
		after.BreakoutPct = clamp(after.BreakoutPct*mulSoft, minBo, maxBo)
	default:
		e.tuneMu.Unlock()
		return models.TuneDecision{
			Changed:  false,
			Why:      models.TuneWhyNoDominant,
			Dominant: dom,
			DomPct:   domPct,
			Total:    total,
			From:     snap.From,
			To:       snap.To,
		}
	}

	changed := (after != before)
	if changed {
		e.tune = after
		e.lastTuneAt = now
		// ❌ lastSignalAt НЕ трогаем
	}
	e.tuneMu.Unlock()

	dec := models.TuneDecision{
		Changed:  changed,
		Why:      models.TuneWhyOK,
		Before:   before,
		After:    after,
		Dominant: dom,
		DomPct:   domPct,
		Total:    total,
		From:     snap.From,
		To:       snap.To,
	}

	if !changed {
		return dec
	}

	_ = e.rejects.Snapshot(true)
	return dec
}

func (e *Service) AutoTuneNow() models.TuneDecision {
	now := time.Now()
	dec := e.MaybeAutoTuneAdaptive(now, true)

	if dec.Changed {
		e.Logger.Info("tune changed",
			zap.String("dom", tuneReasonLabel(dec.Dominant)),
			zap.Float64("dom_pct", dec.DomPct),
			zap.Uint64("total", dec.Total),

			zap.Float64("breakout_before", dec.Before.BreakoutPct),
			zap.Float64("breakout_after", dec.After.BreakoutPct),

			zap.Float64("ch_before", dec.Before.MinChannelPct),
			zap.Float64("ch_after", dec.After.MinChannelPct),

			zap.Float64("body_before", dec.Before.MinBodyPct),
			zap.Float64("body_after", dec.After.MinBodyPct),

			zap.Float64("closeUp_before", dec.Before.CloseUpMin),
			zap.Float64("closeUp_after", dec.After.CloseUpMin),

			zap.Float64("closeDn_before", dec.Before.CloseDnMax),
			zap.Float64("closeDn_after", dec.After.CloseDnMax),
		)
	} else {
		e.Logger.Info("tune skipped",
			zap.String("why", string(dec.Why)),
			zap.String("dom", tuneReasonLabel(dec.Dominant)),
			zap.Float64("dom_pct", dec.DomPct),
			zap.Uint64("total", dec.Total),
			zap.Time("from", dec.From),
			zap.Time("to", dec.To),
		)
	}

	return dec
}
func (e *Service) TuneMode() models.TuneMode {
	e.tuneModeMu.RLock()
	defer e.tuneModeMu.RUnlock()
	return e.tuneMode
}

func (e *Service) SetTuneMode(m models.TuneMode) {
	e.tuneModeMu.Lock()
	e.tuneMode = m
	e.tuneModeMu.Unlock()
}

func (e *Service) ToggleTuneMode() models.TuneMode {
	e.tuneModeMu.Lock()
	defer e.tuneModeMu.Unlock()

	switch e.tuneMode {
	case models.TuneOff:
		e.tuneMode = models.TuneSafe
	case models.TuneSafe:
		e.tuneMode = models.TuneAuto
	default:
		e.tuneMode = models.TuneOff
	}
	return e.tuneMode
}
func (s *Service) LTF() string { return s.cfg.Strategy.LTF }
func (s *Service) HTF() string { return s.cfg.Strategy.HTF }

func (s *Service) LTFNeed() int { return s.cfg.Strategy.DonchianPeriod + 30 }
func (s *Service) HTFNeed() int { return s.cfg.Strategy.HTFEmaSlow + 30 }
