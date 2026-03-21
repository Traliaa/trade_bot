package service

import (
	"log"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"

	"go.uber.org/zap"
)

func (e *Service) getStrategyStateLocked(instID string) *models.StrategyState {
	if e.stateV3 == nil {
		e.stateV3 = make(map[string]*models.StrategyState)
	}

	st, ok := e.stateV3[instID]
	if ok && st != nil {
		return st
	}

	st = &models.StrategyState{}
	e.stateV3[instID] = st
	return st
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func (e *Service) rejectV3(instID string, reason models.RejectReason) {
	st := e.getStrategyStateLocked(instID)
	st.LastRejectReason = reason

	if e.rejects != nil {
		e.rejects.Inc(reason)
	}

	switch reason {
	case models.RejectConfirmScoreLow, models.RejectHTFConflict, models.RejectCompressedRange:
		e.Logger.Info("strategy reject",
			zap.String("strategy", string(e.cfg.Strategy.Name)),
			zap.String("instId", instID),
			zap.String("reason", string(reason)),
		)
	default:
		e.Logger.Debug("strategy reject",
			zap.String("strategy", string(e.cfg.Strategy.Name)),
			zap.String("instId", instID),
			zap.String("reason", string(reason)),
		)
	}
}
func (e *Service) detectRetestLevelsLocked(
	ltf []models.CandleTick,
	htf []models.CandleTick,
) (float64, float64) {

	if len(htf) < 2 || len(ltf) < 3 {
		return 0, 0
	}

	lookback := minInt(20, len(htf)-1)
	prevHTF := htf[:len(htf)-1]

	htfHigh := highestHigh(prevHTF, lookback)
	htfLow := lowestLow(prevHTF, lookback)

	var longLevel float64
	var shortLevel float64

	// --- LONG retest ---
	for i := len(ltf) - 1; i >= 1; i-- {
		c := ltf[i]
		prev := ltf[i-1]

		// breakout вверх
		if prev.Close <= htfHigh && c.Close > htfHigh {
			longLevel = htfHigh
			break
		}

		// fallback: если уже выше уровня
		if c.Close > htfHigh {
			longLevel = htfHigh
			break
		}
	}

	// --- SHORT retest ---
	for i := len(ltf) - 1; i >= 1; i-- {
		c := ltf[i]
		prev := ltf[i-1]

		if prev.Close >= htfLow && c.Close < htfLow {
			shortLevel = htfLow
			break
		}

		if c.Close < htfLow {
			shortLevel = htfLow
			break
		}
	}

	return longLevel, shortLevel
}
func (e *Service) buildLongScore(
	ltf []models.CandleTick,
	mctx models.MarketContext,
	retestLevel float64,
) models.SignalScore {
	_, retestTol, impulseMin, _, closeUpMin, _ := e.effectiveV3Params()

	s := models.SignalScore{
		Reasons: make([]models.RejectReason, 0, 8),
	}

	if len(ltf) < 3 {
		s.Reasons = append(s.Reasons, models.RejectNotEnoughLTFBars)
		return s
	}

	last := ltf[len(ltf)-1]
	prev := ltf[len(ltf)-2]

	bodyPct := 0.0
	if last.Close > 0 {
		bodyPct = candleBody(last) / last.Close
	}

	closePos := closePosInRange(last)

	retestOK := retestLevel > 0 && distancePct(last.Low, retestLevel) <= retestTol
	strongClose := closePos >= closeUpMin
	reclaimOK := retestLevel > 0 && last.Close >= retestLevel*(1-retestTol)
	impulseOK := bodyPct >= impulseMin && last.Close > prev.Close
	structureOK := last.Low >= prev.Low || last.Close > prev.High

	contextOK := true
	if mctx.Bias == models.MarketBiasBear {
		contextOK = false
		s.Reasons = append(s.Reasons, models.RejectHTFConflict)
	}
	if mctx.Compressed {
		contextOK = false
		s.Reasons = append(s.Reasons, models.RejectCompressedRange)
	}
	if mctx.OverextendedUp {
		contextOK = false
		s.Reasons = append(s.Reasons, models.RejectOverextendedUp)
	}

	s.SetupOK = retestOK
	s.ContextOK = contextOK
	s.RetestOK = retestOK
	s.StrongClose = strongClose
	s.ReclaimOK = reclaimOK
	s.ImpulseOK = impulseOK
	s.StructureOK = structureOK
	s.VolatilityOK = mctx.VolatilityOK

	s.Score += boolScore(retestOK)
	s.Score += boolScore(strongClose)
	s.Score += boolScore(reclaimOK)
	s.Score += boolScore(impulseOK)
	s.Score += boolScore(structureOK)

	if !retestOK {
		s.Reasons = append(s.Reasons, models.RejectRetestNotConfirmed)
	}
	if !strongClose {
		s.Reasons = append(s.Reasons, models.RejectWeakCloseUp)
	}
	if !reclaimOK {
		s.Reasons = append(s.Reasons, models.RejectReclaimFailed)
	}
	if !impulseOK {
		s.Reasons = append(s.Reasons, models.RejectImpulseWeak)
	}
	if !structureOK {
		s.Reasons = append(s.Reasons, models.RejectStructureNotConfirmed)
	}
	if !mctx.VolatilityOK {
		s.Reasons = append(s.Reasons, models.RejectVolatilityTooLow)
	}

	return s
}

func (e *Service) buildShortScore(
	ltf []models.CandleTick,
	mctx models.MarketContext,
	retestLevel float64,
) models.SignalScore {
	_, retestTol, impulseMin, _, _, closeDnMax := e.effectiveV3Params()

	s := models.SignalScore{
		Reasons: make([]models.RejectReason, 0, 8),
	}

	if len(ltf) < 3 {
		s.Reasons = append(s.Reasons, models.RejectNotEnoughLTFBars)
		return s
	}

	last := ltf[len(ltf)-1]
	prev := ltf[len(ltf)-2]

	bodyPct := 0.0
	if last.Close > 0 {
		bodyPct = candleBody(last) / last.Close
	}

	closePos := closePosInRange(last)

	retestOK := retestLevel > 0 && distancePct(last.High, retestLevel) <= retestTol
	strongClose := closePos <= closeDnMax
	reclaimOK := retestLevel > 0 && last.Close <= retestLevel*(1+retestTol)
	impulseOK := bodyPct >= impulseMin && last.Close < prev.Close
	structureOK := last.High <= prev.High || last.Close < prev.Low

	contextOK := true
	if mctx.Bias == models.MarketBiasBull {
		contextOK = false
		s.Reasons = append(s.Reasons, models.RejectHTFConflict)
	}
	if mctx.Compressed {
		contextOK = false
		s.Reasons = append(s.Reasons, models.RejectCompressedRange)
	}
	if mctx.OverextendedDown {
		contextOK = false
		s.Reasons = append(s.Reasons, models.RejectOverextendedDown)
	}

	s.SetupOK = retestOK
	s.ContextOK = contextOK
	s.RetestOK = retestOK
	s.StrongClose = strongClose
	s.ReclaimOK = reclaimOK
	s.ImpulseOK = impulseOK
	s.StructureOK = structureOK
	s.VolatilityOK = mctx.VolatilityOK

	s.Score += boolScore(retestOK)
	s.Score += boolScore(strongClose)
	s.Score += boolScore(reclaimOK)
	s.Score += boolScore(impulseOK)
	s.Score += boolScore(structureOK)

	if !retestOK {
		s.Reasons = append(s.Reasons, models.RejectRetestNotConfirmed)
	}
	if !strongClose {
		s.Reasons = append(s.Reasons, models.RejectWeakCloseDown)
	}
	if !reclaimOK {
		s.Reasons = append(s.Reasons, models.RejectReclaimFailed)
	}
	if !impulseOK {
		s.Reasons = append(s.Reasons, models.RejectImpulseWeak)
	}
	if !structureOK {
		s.Reasons = append(s.Reasons, models.RejectStructureNotConfirmed)
	}
	if !mctx.VolatilityOK {
		s.Reasons = append(s.Reasons, models.RejectVolatilityTooLow)
	}

	return s
}

func (e *Service) OnCandleV3(t models.CandleTick) (models.Signal, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.lastTickAt = time.Now()

	instID := t.InstID
	mst := e.getV3MarketStateLocked(instID)

	tf := helper.NormTF(t.TimeframeRaw)

	switch tf {
	case helper.NormTF(e.cfg.Strategy.HTF):
		mst.HTFCandles = appendCappedCandles(mst.HTFCandles, t, 200)
		return models.Signal{}, false

	case helper.NormTF(e.cfg.Strategy.LTF):
		mst.LTFCandles = appendCappedCandles(mst.LTFCandles, t, 200)
		return e.onCandleV3ReadyLocked(t, mst)

	default:
		return models.Signal{}, false
	}
}
func (e *Service) getV3MarketStateLocked(instID string) *models.V3MarketState {
	if e.stV3 == nil {
		e.stV3 = make(map[string]*models.V3MarketState)
	}

	st, ok := e.stV3[instID]
	if ok && st != nil {
		return st
	}

	st = &models.V3MarketState{}
	e.stV3[instID] = st
	return st
}
func (e *Service) onCandleV3ReadyLocked(
	t models.CandleTick,
	mst *models.V3MarketState,
) (models.Signal, bool) {
	var zero models.Signal

	instID := t.InstID
	v3st := e.getStrategyStateLocked(instID)

	if mst == nil {
		e.rejectV3(instID, models.RejectStateNil)
		return zero, false
	}
	// Уже есть активная позиция/сделка по инструменту — новый вход не даём.
	// Иначе стратегия может набрать несколько входов подряд по одному instID.
	if v3st.EntryPrice > 0 && v3st.InitialRisk > 0 {
		e.rejectV3(instID, models.RejectNotReady)
		return zero, false
	}

	if len(mst.LTFCandles) < 20 || len(mst.HTFCandles) < 10 {
		e.rejectV3(instID, models.RejectNotEnoughCandles)
		return zero, false
	}

	last := mst.LTFCandles[len(mst.LTFCandles)-1]

	if !mst.CooldownUntil.IsZero() &&
		(t.End.Before(mst.CooldownUntil) || t.End.Equal(mst.CooldownUntil)) {
		e.rejectV3(instID, models.RejectCooldown)
		return zero, false
	}

	if !mst.LastSignalEnd.IsZero() && mst.LastSignalEnd.Equal(last.End) {
		e.rejectV3(instID, models.RejectAlreadySignaledThisBar)
		return zero, false
	}

	minConfirm, _, _, _, _, _ := e.effectiveV3Params()

	mctx := e.buildMarketContext(mst.LTFCandles, mst.HTFCandles)
	longRetestLevel, shortRetestLevel := e.detectRetestLevelsLocked(mst.LTFCandles, mst.HTFCandles)

	longScore := e.buildLongScore(mst.LTFCandles, mctx, longRetestLevel)
	shortScore := e.buildShortScore(mst.LTFCandles, mctx, shortRetestLevel)

	e.Logger.Debug("v3 score",
		zap.String("instId", instID),
		zap.String("bias", string(mctx.Bias)),
		zap.Bool("compressed", mctx.Compressed),
		zap.Int("min_confirm", minConfirm),
		zap.Int("long_score", longScore.Score),
		zap.Int("short_score", shortScore.Score),
		zap.Strings("long_reasons", rejectReasonsToStrings(longScore.Reasons)),
		zap.Strings("short_reasons", rejectReasonsToStrings(shortScore.Reasons)),
	)

	if longScore.SetupOK && longScore.ContextOK && longScore.Score >= minConfirm {
		v3st.LastSignalScore = longScore.Score
		v3st.LastRetestLevel = longRetestLevel
		v3st.LastRejectReason = ""

		mst.LastSignalEnd = last.End
		e.lastSignalAt = time.Now()

		return models.Signal{
			InstID:     instID,
			TF:         e.cfg.Strategy.LTF,
			Side:       models.SideBuy,
			Price:      last.Close,
			Strategy:   models.StrategyDonchianV3,
			Reason:     buildV3Reason("v3_long", longScore.Score, mctx.Bias, longRetestLevel, mctx.ChannelWidthPct, mctx.Compressed),
			CreatedAt:  time.Now(),
			LTFCandles: lastNCandles(mst.LTFCandles, 30),
			HTFCandles: lastNCandles(mst.HTFCandles, 30),
		}, true
	}

	if shortScore.SetupOK && shortScore.ContextOK && shortScore.Score >= minConfirm {
		v3st.LastSignalScore = shortScore.Score
		v3st.LastRetestLevel = shortRetestLevel
		v3st.LastRejectReason = ""

		mst.LastSignalEnd = last.End
		e.lastSignalAt = time.Now()

		return models.Signal{
			InstID:     instID,
			TF:         e.cfg.Strategy.LTF,
			Side:       models.SideSell,
			Price:      last.Close,
			Strategy:   models.StrategyDonchianV3,
			Reason:     buildV3Reason("v3_short", shortScore.Score, mctx.Bias, shortRetestLevel, mctx.ChannelWidthPct, mctx.Compressed),
			CreatedAt:  time.Now(),
			LTFCandles: lastNCandles(mst.LTFCandles, 30),
			HTFCandles: lastNCandles(mst.HTFCandles, 30),
		}, true
	}

	if longScore.Score >= shortScore.Score {
		e.rejectV3(instID, firstReasonOr(longScore.Reasons, models.RejectConfirmScoreLow))
	} else {
		e.rejectV3(instID, firstReasonOr(shortScore.Reasons, models.RejectConfirmScoreLow))
	}

	return zero, false
}

func (e *Service) AutoTuneV3Now(mode models.TuneMode) models.TuneDecision {
	now := time.Now()

	// не слишком часто
	minTuneGap := 30 * time.Minute
	if mode == models.TuneManual {
		minTuneGap = 5 * time.Minute
	}

	if !e.lastTuneAt.IsZero() && now.Sub(e.lastTuneAt) < minTuneGap {
		return models.TuneDecision{
			Changed: false,
			Why:     models.TuneWhyCooldown,
		}
	}

	// только читаем, без reset
	snap := e.snapshotV3Rejects(false)
	if snap.Total < 50 {
		return models.TuneDecision{
			Changed: false,
			Why:     models.TuneWhyTooFewRejects,
		}
	}

	dom, domCount := dominantRejectReason(snap.Counts)
	if domCount <= 0 {
		return models.TuneDecision{
			Changed: false,
			Why:     models.TuneWhyNoDominantReason,
		}
	}

	domPct := float64(domCount) / float64(snap.Total)
	if domPct < 0.45 {
		return models.TuneDecision{
			Changed:  false,
			Why:      models.TuneWhyNoDominantReason,
			Dominant: dom,
			Total:    uint64(snap.Total),
			DomPct:   domPct,
		}
	}

	e.tuneMu.Lock()
	defer e.tuneMu.Unlock()

	before := e.effectiveV3TuningLocked()
	after := before
	changed := false

	switch dom {
	case models.RejectConfirmScoreLow:
		newVal := clampInt(after.V3MinConfirmScore-1, 2, 5)
		if newVal != after.V3MinConfirmScore {
			after.V3MinConfirmScore = newVal
			changed = true
		}

	case models.RejectRetestNotConfirmed:
		newVal := clampFloat(after.V3RetestTolerancePct*1.10, 0.0008, 0.0050)
		if !almostEqual(after.V3RetestTolerancePct, newVal) {
			after.V3RetestTolerancePct = newVal
			changed = true
		}

	case models.RejectImpulseWeak:
		newVal := clampFloat(after.V3ImpulseBodyMinPct*0.90, 0.0010, 0.0100)
		if !almostEqual(after.V3ImpulseBodyMinPct, newVal) {
			after.V3ImpulseBodyMinPct = newVal
			changed = true
		}

	case models.RejectCompressedRange, models.RejectVolatilityTooLow:
		newVal := clampFloat(after.V3CompressionThresholdPct*0.90, 0.0030, 0.0200)
		if !almostEqual(after.V3CompressionThresholdPct, newVal) {
			after.V3CompressionThresholdPct = newVal
			changed = true
		}

	case models.RejectWeakCloseUp:
		newVal := clampFloat(after.V3StrongCloseMin-0.03, 0.55, 0.85)
		if !almostEqual(after.V3StrongCloseMin, newVal) {
			after.V3StrongCloseMin = newVal
			changed = true
		}

	case models.RejectWeakCloseDown:
		newVal := clampFloat(after.V3StrongCloseMax+0.03, 0.15, 0.45)
		if !almostEqual(after.V3StrongCloseMax, newVal) {
			after.V3StrongCloseMax = newVal
			changed = true
		}

	// эти причины пока не тюним автоматически
	case models.RejectHTFConflict,
		models.RejectOverextendedUp,
		models.RejectOverextendedDown,
		models.RejectStructureNotConfirmed,
		models.RejectReclaimFailed,
		models.RejectNotReady,
		models.RejectCooldown,
		models.RejectNotEnoughCandles,
		models.RejectAlreadySignaledThisBar,
		models.RejectNotEnoughLTFBars:
		changed = false

	default:
		changed = false
	}

	if !changed {
		return models.TuneDecision{
			Changed:  false,
			Why:      models.TuneWhyNoChange,
			Dominant: dom,
			Total:    uint64(snap.Total),
			DomPct:   domPct,
			Before:   before,
			After:    after,
		}
	}

	e.tune = after
	e.lastTuneAt = now

	log.Printf(
		"[TUNE V3] changed dom=%s pct=%.0f%% total=%d | score %d->%d retest %.5f->%.5f impulse %.5f->%.5f compression %.5f->%.5f closeUp %.2f->%.2f closeDn %.2f->%.2f",
		rejectReasonLabel(dom), domPct*100, snap.Total,
		before.V3MinConfirmScore, after.V3MinConfirmScore,
		before.V3RetestTolerancePct, after.V3RetestTolerancePct,
		before.V3ImpulseBodyMinPct, after.V3ImpulseBodyMinPct,
		before.V3CompressionThresholdPct, after.V3CompressionThresholdPct,
		before.V3StrongCloseMin, after.V3StrongCloseMin,
		before.V3StrongCloseMax, after.V3StrongCloseMax,
	)

	return models.TuneDecision{
		Changed:  true,
		Why:      models.TuneWhyAdjusted,
		Dominant: dom,
		Total:    uint64(snap.Total),
		DomPct:   domPct,
		Before:   before,
		After:    after,
	}
}
func (e *Service) snapshotV3Rejects(reset bool) rejectSnapshot {
	if e.rejects == nil {
		return rejectSnapshot{
			Total:  0,
			Counts: map[models.RejectReason]int{},
		}
	}

	src := e.rejects.Snapshot(reset)

	out := rejectSnapshot{
		Counts: make(map[models.RejectReason]int),
	}

	for reason, cnt := range src.Counts {
		if !isV3RejectReason(reason) {
			continue
		}
		out.Counts[reason] = int(cnt)
		out.Total += int(cnt)
	}

	return out
}

func (e *Service) effectiveV3TuningLocked() models.RuntimeTuning {
	cfg := e.cfg.Strategy
	cfg.ApplyV3Defaults()

	t := e.tune

	if t.V3MinConfirmScore <= 0 {
		t.V3MinConfirmScore = cfg.MinConfirmScore
	}
	if t.V3RetestTolerancePct <= 0 {
		t.V3RetestTolerancePct = cfg.RetestTolerancePct
	}
	if t.V3ImpulseBodyMinPct <= 0 {
		t.V3ImpulseBodyMinPct = cfg.ImpulseBodyMinPct
	}
	if t.V3CompressionThresholdPct <= 0 {
		t.V3CompressionThresholdPct = cfg.CompressionThresholdPct
	}
	if t.V3StrongCloseMin <= 0 {
		t.V3StrongCloseMin = cfg.StrongCloseMin
	}
	if t.V3StrongCloseMax <= 0 {
		t.V3StrongCloseMax = cfg.StrongCloseMax
	}

	return t
}
func (e *Service) effectiveV3Params() (
	minConfirm int,
	retestTol float64,
	impulseMin float64,
	compression float64,
	closeUpMin float64,
	closeDnMax float64,
) {
	e.tuneMu.RLock()
	defer e.tuneMu.RUnlock()

	cfg := e.cfg.Strategy
	cfg.ApplyV3Defaults()

	minConfirm = cfg.MinConfirmScore
	retestTol = cfg.RetestTolerancePct
	impulseMin = cfg.ImpulseBodyMinPct
	compression = cfg.CompressionThresholdPct
	closeUpMin = cfg.StrongCloseMin
	closeDnMax = cfg.StrongCloseMax

	if e.tune.V3MinConfirmScore > 0 {
		minConfirm = e.tune.V3MinConfirmScore
	}
	if e.tune.V3RetestTolerancePct > 0 {
		retestTol = e.tune.V3RetestTolerancePct
	}
	if e.tune.V3ImpulseBodyMinPct > 0 {
		impulseMin = e.tune.V3ImpulseBodyMinPct
	}
	if e.tune.V3CompressionThresholdPct > 0 {
		compression = e.tune.V3CompressionThresholdPct
	}
	if e.tune.V3StrongCloseMin > 0 {
		closeUpMin = e.tune.V3StrongCloseMin
	}
	if e.tune.V3StrongCloseMax > 0 {
		closeDnMax = e.tune.V3StrongCloseMax
	}

	return
}
func (e *Service) buildMarketContext(
	ltf []models.CandleTick,
	htf []models.CandleTick,
) models.MarketContext {
	_, _, _, compression, _, _ := e.effectiveV3Params()

	ctx := models.MarketContext{
		Bias:         models.MarketBiasNeutral,
		VolatilityOK: true,
	}

	if len(ltf) < 5 || len(htf) < 3 {
		return ctx
	}

	lastLTF := ltf[len(ltf)-1]
	lastHTF := htf[len(htf)-1]

	histHTF := htf[:len(htf)-1]
	if len(histHTF) < 2 {
		return ctx
	}

	htfHigh := highestHigh(histHTF, minInt(20, len(histHTF)))
	htfLow := lowestLow(histHTF, minInt(20, len(histHTF)))
	htfMid := (htfHigh + htfLow) / 2

	if htfLow > 0 && htfHigh > htfLow {
		ctx.ChannelWidthPct = (htfHigh - htfLow) / htfLow
	}
	if htfMid > 0 {
		ctx.DistanceToMidPct = abs(lastLTF.Close-htfMid) / htfMid
	}

	if lastHTF.Close > htfMid {
		ctx.Bias = models.MarketBiasBull
	} else if lastHTF.Close < htfMid {
		ctx.Bias = models.MarketBiasBear
	}

	if ctx.ChannelWidthPct > 0 && ctx.ChannelWidthPct < compression {
		ctx.Compressed = true
		ctx.VolatilityOK = false
	}

	if htfHigh > 0 && distancePct(lastLTF.Close, htfHigh) < 0.001 {
		ctx.OverextendedUp = true
	}
	if htfLow > 0 && distancePct(lastLTF.Close, htfLow) < 0.001 {
		ctx.OverextendedDown = true
	}

	ctx.TrendStrength = ctx.ChannelWidthPct

	return ctx
}
func (e *Service) manageOpenPositionV3(
	instID string,
	st *models.StrategyState,
	last models.CandleTick,
	side string,
) {
	if st == nil || st.EntryPrice <= 0 || st.InitialRisk <= 0 {
		return
	}

	price := last.Close
	r := st.InitialRisk

	// =========================
	// 1. PARTIAL + BE
	// =========================

	switch side {
	case "BUY":
		if !st.PartialDone && price >= st.EntryPrice+r {
			st.PartialDone = true
			st.BEActivated = true
			st.TrailingStop = st.EntryPrice

			e.Logger.Info("v3 partial TP + BE",
				zap.String("instId", instID),
				zap.Float64("price", price),
			)

			// TODO: вызвать частичное закрытие позиции
		}

	case "SELL":
		if !st.PartialDone && price <= st.EntryPrice-r {
			st.PartialDone = true
			st.BEActivated = true
			st.TrailingStop = st.EntryPrice

			e.Logger.Info("v3 partial TP + BE",
				zap.String("instId", instID),
				zap.Float64("price", price),
			)
		}
	}

	// =========================
	// 2. TRAILING
	// =========================

	if !st.BEActivated {
		return
	}

	switch side {
	case "BUY":
		newStop := last.Low
		if newStop > st.TrailingStop {
			st.TrailingStop = newStop
		}

	case "SELL":
		newStop := last.High
		if st.TrailingStop == 0 || newStop < st.TrailingStop {
			st.TrailingStop = newStop
		}
	}
}
