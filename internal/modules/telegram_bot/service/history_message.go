package service

import (
	"fmt"
	"strings"

	"trade_bot/internal/models"
)

func formatHistoryMessage(trades []models.TradeRecord) string {
	if len(trades) == 0 {
		return "📚 История сделок пока пустая."
	}

	var b strings.Builder
	b.WriteString("📚 Последние сделки\n\n")

	for i, tr := range trades {
		fmt.Fprintf(&b, "%d. %s | %s\n", i+1, tr.InstID, strings.ToUpper(tr.PosSide))

		switch tr.Status {
		case models.TradeStatusOpen:
			fmt.Fprintf(&b, "   Статус: OPEN\n")
			fmt.Fprintf(&b, "   Вход: %.6f\n", tr.EntryPrice)
			fmt.Fprintf(&b, "   Размер: %.4f\n", tr.EntrySize)
		case models.TradeStatusClosed:
			fmt.Fprintf(&b, "   PnL: %+.2f USDT (%+.2f%%)\n", tr.RealizedPnL, tr.RealizedPnLPct)
			fmt.Fprintf(&b, "   Причина: %s\n", strings.ToUpper(string(tr.CloseReason)))
			fmt.Fprintf(&b, "   Вход/выход: %.6f → %.6f\n", tr.EntryPrice, tr.ExitPrice)
		default:
			fmt.Fprintf(&b, "   Статус: %s\n", strings.ToUpper(string(tr.Status)))
		}

		if i != len(trades)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}
