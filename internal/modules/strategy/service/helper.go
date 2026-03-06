package service

import (
	"strconv"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

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
func itoa(i int) string { return strconv.Itoa(i) }

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
func emaUpdate(prev, x float64, alpha float64) float64 {
	if prev == 0 {
		return x
	}
	return prev + alpha*(x-prev)
}

func clampFloat(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
func tfDuration(tf string) time.Duration {
	switch helper.NormTF(tf) {
	case "1m":
		return time.Minute
	case "3m":
		return 3 * time.Minute
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "2h":
		return 2 * time.Hour
	case "4h":
		return 4 * time.Hour
	default:
		// безопасный дефолт, чтобы pending не умирал мгновенно
		return 15 * time.Minute
	}
}

const pendingMaxAdversePct = 0.004
const pendingCooldownBars = 2

func adversePct(side models.Side, level, price float64) float64 {
	if level <= 0 || price <= 0 {
		return 0
	}
	// adverse = движение ПРОТИВ нас
	if side == models.SideBuy {
		// для buy плохо, когда цена ниже level
		if price >= level {
			return 0
		}
		return (level - price) / level
	}
	// sell: плохо, когда цена выше level
	if price <= level {
		return 0
	}
	return (price - level) / level
}
func isCooldownActive(until time.Time) bool {
	return !until.IsZero() && time.Now().Before(until)
}
