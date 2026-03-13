package sessions

import (
	"fmt"
	"strings"
	"time"

	"trade_bot/internal/models"
)

func formatClosedTradeMessage(tr models.TradeRecord, in models.TradeCloseInput) string {
	var b strings.Builder

	title := "✅ СДЕЛКА ЗАКРЫТА"
	switch in.CloseReason {
	case models.CloseReasonTP:
		title = "🎯 ТЕЙК-ПРОФИТ"
	case models.CloseReasonSL:
		title = "🛑 СТОП-ЛОСС"
	case models.CloseReasonTimeStop:
		title = "🕒 TIME STOP"
	}

	b.WriteString(title)
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "Инструмент: %s\n", tr.InstID)
	fmt.Fprintf(&b, "Направление: %s\n", strings.ToUpper(tr.PosSide))

	if tr.Strategy != "" {
		fmt.Fprintf(&b, "Стратегия: %s\n", tr.Strategy)
	}
	if tr.Timeframe != "" {
		fmt.Fprintf(&b, "ТФ: %s\n", tr.Timeframe)
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "Вход: %.6f\n", tr.EntryPrice)
	fmt.Fprintf(&b, "Выход: %.6f\n", in.ExitPrice)
	fmt.Fprintf(&b, "Размер: %.4f\n", in.ExitSize)

	b.WriteString("\n")
	fmt.Fprintf(&b, "PnL: %+.2f USDT\n", in.RealizedPnL)
	fmt.Fprintf(&b, "Результат: %+.2f%%\n", in.RealizedPnLPct)
	fmt.Fprintf(&b, "Причина: %s\n", strings.ToUpper(string(in.CloseReason)))

	dur := in.ExitAt.Sub(tr.EntryAt).Round(time.Minute)
	if dur > 0 {
		fmt.Fprintf(&b, "Длительность: %s\n", formatTradeDuration(dur))
	}

	return b.String()
}

func formatTradeDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

	h := int(d.Hours())
	m := int(d.Minutes()) % 60

	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}

	return fmt.Sprintf("%dh %dm", h, m)
}
