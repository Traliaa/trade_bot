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
func dominantReason(s models.RejectSnapshot) (reason models.RejectReason, pct float64, total uint64) {
	total = s.Total
	if total == 0 {
		return "", 0, 0
	}

	var weakClose uint64
	var top models.RejectReason
	var topCnt uint64

	for r, c := range s.By {
		if isWeakClose(r) {
			weakClose += c
			continue
		}
		if c > topCnt {
			topCnt = c
			top = r
		}
	}

	if weakClose > topCnt {
		topCnt = weakClose
		top = models.RejectWeakCloseUp // маркер aggregated weak_close
	}

	return top, float64(topCnt) / float64(total), total
}

// label для красивых логов/админки
func tuneReasonLabel(r models.RejectReason) string {
	if r == models.RejectWeakCloseUp {
		return "weak_close (up+down)"
	}
	return string(r)
}
