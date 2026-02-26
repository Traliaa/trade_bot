package service

import (
	"fmt"
	"strconv"
	"time"
	"trade_bot/internal/models"
)

func FormatTuneDecision(dec models.TuneDecision, warmupDone bool, lastSignalAt, lastTuneAt time.Time, cur models.RuntimeTuning, mode models.TuneMode) string {
	modeStr := models.TuneModeString(mode)

	ls := "—"
	if !lastSignalAt.IsZero() {
		ls = lastSignalAt.Format("15:04:05")
	}
	lt := "—"
	if !lastTuneAt.IsZero() {
		lt = lastTuneAt.Format("15:04:05")
	}

	win := "—"
	if !dec.From.IsZero() || !dec.To.IsZero() {
		win = dec.From.Format("15:04:05") + " — " + dec.To.Format("15:04:05")
	}

	dom := "—"
	if dec.Dominant != "" {
		dom = tuneReasonLabel(dec.Dominant)
	}

	header := "⚙️ Авто-тюн\n"
	header += "Режим: " + modeStr + "\n"
	header += "Warmup: " + map[bool]string{true: "✅ done", false: "⏳ not done"}[warmupDone] + "\n"
	header += "LastSignalAt: " + ls + "\n"
	header += "LastTuneAt: " + lt + "\n\n"

	why := "Решение: "
	if dec.Changed {
		why += "✅ применено\n"
	} else {
		why += "⏭ пропущено (" + string(dec.Why) + ")\n"
	}

	stats := ""
	if dec.Total > 0 {
		stats += "Окно rejects: " + win + "\n"
		stats += "Всего отклонений: " + itoaU64(dec.Total) + "\n"
		stats += "Доминирует: " + dom + " (" + fmtPct(dec.DomPct) + ")\n\n"
	}

	curBlock := "Текущие пороги:\n" +
		fmt.Sprintf("• MinChannelPct: %.4f\n", cur.MinChannelPct) +
		fmt.Sprintf("• MinBodyPct:    %.4f\n", cur.MinBodyPct) +
		fmt.Sprintf("• BreakoutPct:   %.4f\n", cur.BreakoutPct) +
		fmt.Sprintf("• CloseUpMin:    %.2f\n", cur.CloseUpMin) +
		fmt.Sprintf("• CloseDnMax:    %.2f\n", cur.CloseDnMax)

	if !dec.Changed {
		return header + why + stats + curBlock
	}

	// Показать только реально изменившиеся
	changes := "Изменения:\n"
	changes += diffLine("MinChannelPct", dec.Before.MinChannelPct, dec.After.MinChannelPct, 4)
	changes += diffLine("MinBodyPct", dec.Before.MinBodyPct, dec.After.MinBodyPct, 4)
	changes += diffLine("BreakoutPct", dec.Before.BreakoutPct, dec.After.BreakoutPct, 4)
	changes += diffLine("CloseUpMin", dec.Before.CloseUpMin, dec.After.CloseUpMin, 2)
	changes += diffLine("CloseDnMax", dec.Before.CloseDnMax, dec.After.CloseDnMax, 2)

	return header + why + stats + changes + "\n" + curBlock
}

func fmtPct(p float64) string {
	if p <= 0 {
		return "0%"
	}
	v := int(p*100 + 0.5)
	return fmt.Sprintf("%d%%", v)
}

func diffLine(name string, a, b float64, prec int) string {
	if a == b {
		return ""
	}
	format := "%." + strconv.Itoa(prec) + "f"
	return fmt.Sprintf("• %s: "+format+" → "+format+"\n", name, a, b)
}
func itoaU64(v uint64) string {
	return strconv.FormatUint(v, 10)
}
func tuneReasonLabel(r models.RejectReason) string {
	if r == models.RejectWeakCloseUp {
		return "weak_close (up+down)"
	}
	return string(r)
}
