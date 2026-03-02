package service

import (
	"strconv"
	"trade_bot/internal/models"
)

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
func maxSlice(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minSlice(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
func itoaU64(v uint64) string {
	return strconv.FormatUint(v, 10)
}
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isWeakClose(r models.RejectReason) bool {
	return r == models.RejectWeakCloseUp || r == models.RejectWeakCloseDown
}

// dominantReason выбирает доминирующий “душитель” по снапшоту.
// weak_close_up + weak_close_down агрегируются в одну причину (маркер RejectWeakCloseUp).
func dominantReason(snap models.RejectSnapshot) (models.RejectReason, float64, uint64) {
	total := snap.Total
	if total == 0 {
		return "", 0, 0
	}

	// 1) обычная доминанта среди raw- причин
	var bestR models.RejectReason
	var bestC uint64
	for r, c := range snap.By {
		if c > bestC {
			bestC = c
			bestR = r
		}
	}

	// 2) aggregated weak_close = up + down
	weakClose := snap.By[models.RejectWeakCloseUp] + snap.By[models.RejectWeakCloseDown]
	if weakClose > bestC {
		bestC = weakClose
		bestR = models.RejectWeakClose
	}

	if bestC == 0 {
		return "", 0, total
	}

	return bestR, float64(bestC) / float64(total), total
}

// label для красивых логов/админки
func tuneReasonLabel(r models.RejectReason) string {
	if r == models.RejectWeakCloseUp {
		return "weak_close (up+down)"
	}
	return string(r)
}
