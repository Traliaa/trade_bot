package service

import (
	"fmt"
	"math"
	"trade_bot/internal/models"
)

func candleRange(c models.CandleTick) float64 {
	return c.High - c.Low
}

func candleBody(c models.CandleTick) float64 {
	return math.Abs(c.Close - c.Open)
}

func closePosInRange(c models.CandleTick) float64 {
	r := candleRange(c)
	if r <= 0 {
		return 0.5
	}
	return (c.Close - c.Low) / r
}

func distancePct(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return math.Abs(a-b) / math.Abs(b)
}

func boolScore(ok bool) int {
	if ok {
		return 1
	}
	return 0
}

func highestHigh(candles []models.CandleTick, n int) float64 {
	if len(candles) == 0 {
		return 0
	}
	if n <= 0 || n > len(candles) {
		n = len(candles)
	}

	start := len(candles) - n
	h := candles[start].High
	for i := start + 1; i < len(candles); i++ {
		if candles[i].High > h {
			h = candles[i].High
		}
	}
	return h
}

func lowestLow(candles []models.CandleTick, n int) float64 {
	if len(candles) == 0 {
		return 0
	}
	if n <= 0 || n > len(candles) {
		n = len(candles)
	}

	start := len(candles) - n
	l := candles[start].Low
	for i := start + 1; i < len(candles); i++ {
		if candles[i].Low < l {
			l = candles[i].Low
		}
	}
	return l
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func firstReasonOr(reasons []models.RejectReason, fallback models.RejectReason) models.RejectReason {
	if len(reasons) == 0 {
		return fallback
	}
	return reasons[0]
}
func rejectReasonsToStrings(in []models.RejectReason) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i := range in {
		out[i] = string(in[i])
	}
	return out
}
func lastNCandles(in []models.CandleTick, n int) []models.CandleTick {
	if len(in) <= n {
		out := make([]models.CandleTick, len(in))
		copy(out, in)
		return out
	}
	out := make([]models.CandleTick, n)
	copy(out, in[len(in)-n:])
	return out
}
func dominantRejectReason(counts map[models.RejectReason]int) (models.RejectReason, int) {
	var dom models.RejectReason
	var maxCnt int

	for reason, cnt := range counts {
		if cnt > maxCnt {
			dom = reason
			maxCnt = cnt
		}
	}

	return dom, maxCnt
}

func clampInt(v, minV, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func rejectReasonLabel(r models.RejectReason) string {
	return string(r)
}
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-12
}

type rejectSnapshot struct {
	Total  int
	Counts map[models.RejectReason]int
}

func isV3RejectReason(r models.RejectReason) bool {
	switch r {
	case models.RejectConfirmScoreLow,
		models.RejectRetestNotConfirmed,
		models.RejectImpulseWeak,
		models.RejectCompressedRange,
		models.RejectVolatilityTooLow,
		models.RejectWeakCloseUp,
		models.RejectWeakCloseDown,
		models.RejectHTFConflict,
		models.RejectReclaimFailed,
		models.RejectStructureNotConfirmed,
		models.RejectOverextendedUp,
		models.RejectOverextendedDown,
		models.RejectLowVolume:
		return true
	default:
		return false
	}
}
func buildV3Reason(
	side string,
	score int,
	bias models.MarketBias,
	retestLevel float64,
	channelWidthPct float64,
	compressed bool,
) string {
	return fmt.Sprintf(
		"%s score=%d bias=%s retest=%.6f ch=%.4f compressed=%t",
		side,
		score,
		string(bias),
		retestLevel,
		channelWidthPct,
		compressed,
	)
}

// weightedRetest оценивает силу retest:
// 0 — нет retest, 1 — wick-touch, 2 — close/body overlap.
func weightedRetest(retestOK bool, c models.CandleTick, retestLevel float64, side models.Side) int {
	if !retestOK {
		return 0
	}

	switch side {
	case models.SideBuy:
		if c.Low > retestLevel || c.Close > retestLevel {
			return 2
		}
	case models.SideSell:
		if c.High < retestLevel || c.Close < retestLevel {
			return 2
		}
	}
	return 1
}

// weightedCloseLong оценивает силу close для long:
// 0 — close в середине, 1 — в верхней трети, 2 — у экстремума (top 10%).
func weightedCloseLong(closePos, closeUpMin float64) int {
	if closePos < closeUpMin {
		return 0
	}
	if closePos >= closeUpMin+0.15 {
		return 2
	}
	return 1
}

// weightedCloseShort оценивает силу close для short:
// 0 — close в середине, 1 — в нижней трети, 2 — у экстремума (bottom 10%).
func weightedCloseShort(closePos, closeDnMax float64) int {
	if closePos > closeDnMax {
		return 0
	}
	if closePos <= closeDnMax-0.15 {
		return 2
	}
	return 1
}

// weightedImpulse оценивает силу импульса:
// 0 — body < min, 1 — body > min, 2 — body > 2×min.
func weightedImpulse(bodyPct, impulseMin float64) int {
	if bodyPct < impulseMin {
		return 0
	}
	if bodyPct >= 2*impulseMin {
		return 2
	}
	return 1
}

func directionalImpulse(
	last models.CandleTick,
	prev models.CandleTick,
	impulseMin float64,
	side models.Side,
) bool {
	if last.Close <= 0 || candleBody(last)/last.Close < impulseMin {
		return false
	}

	switch side {
	case models.SideBuy:
		return last.Close > last.Open && last.Close > prev.Close
	case models.SideSell:
		return last.Close < last.Open && last.Close < prev.Close
	default:
		return false
	}
}

// volumeConfirmation returns the current/SMA ratio, a soft score penalty and
// whether the volume is too low to allow an entry. The hard floor is half of
// the configured confirmation ratio.
func volumeConfirmation(
	candles []models.CandleTick,
	period int,
	minRatio float64,
) (ratio float64, penalty int, hardReject bool) {
	if len(candles) == 0 || period <= 0 || minRatio <= 0 {
		return 0, 0, false
	}

	avgVol := computeSMAVolume(candles, period)
	if avgVol <= 0 {
		return 0, 0, false
	}

	lastVolume := candles[len(candles)-1].Volume
	ratio = lastVolume / avgVol
	if ratio >= minRatio {
		return ratio, 0, false
	}

	return ratio, 1, ratio < minRatio*0.5
}

// computeSMAVolume вычисляет SMA объёма за последние period свечей.
func computeSMAVolume(candles []models.CandleTick, period int) float64 {
	if len(candles) == 0 || period <= 0 {
		return 0
	}
	if len(candles) < period {
		period = len(candles)
	}
	start := len(candles) - period
	var sum float64
	for i := start; i < len(candles); i++ {
		sum += candles[i].Volume
	}
	return sum / float64(period)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func buildSignalDiagnostics(
	side models.Side,
	score models.SignalScore,
	opposite models.SignalScore,
	mctx models.MarketContext,
	retestLevel float64,
	ltf []models.CandleTick,
) models.SignalDiagnostics {
	d := models.SignalDiagnostics{
		Score:           score.Score,
		OppositeScore:   opposite.Score,
		RetestLevel:     retestLevel,
		HTFBias:         mctx.Bias,
		ChannelWidthPct: mctx.ChannelWidthPct,
		Compressed:      mctx.Compressed,
		VolatilityOK:    mctx.VolatilityOK,
		VolumeRatio:     score.VolumeRatio,
		RetestScore:     score.RetestScore,
		CloseScore:      score.CloseScore,
		ReclaimScore:    score.ReclaimScore,
		ImpulseScore:    score.ImpulseScore,
		StructureScore:  score.StructureScore,
	}

	if len(ltf) == 0 {
		return d
	}

	last := ltf[len(ltf)-1]
	if last.Close > 0 {
		d.ImpulseBodyPct = candleBody(last) / last.Close
	}
	d.ClosePos = closePosInRange(last)

	if retestLevel > 0 {
		switch side {
		case models.SideBuy:
			d.RetestDistancePct = distancePct(last.Low, retestLevel)
		case models.SideSell:
			d.RetestDistancePct = distancePct(last.High, retestLevel)
		}
	}

	return d
}
