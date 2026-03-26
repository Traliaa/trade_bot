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
		models.RejectOverextendedDown:
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
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
