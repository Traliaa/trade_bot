package service

import (
	"fmt"
	"strings"

	"trade_bot/internal/models"
)

func formatStatsMessage(st models.TradeStats) string {
	var b strings.Builder

	b.WriteString("📊 Статистика сделок\n\n")

	fmt.Fprintf(&b, "Всего сделок: %d\n", st.TotalTrades)
	fmt.Fprintf(&b, "Открытых: %d\n", st.OpenTrades)
	fmt.Fprintf(&b, "Закрытых: %d\n\n", st.ClosedTrades)

	fmt.Fprintf(&b, "Побед: %d\n", st.Wins)
	fmt.Fprintf(&b, "Убытков: %d\n", st.Losses)
	fmt.Fprintf(&b, "Winrate: %.2f%%\n\n", st.WinRatePct)

	fmt.Fprintf(&b, "Total PnL: %+.2f USDT\n", st.TotalPnL)
	fmt.Fprintf(&b, "Avg PnL: %+.2f USDT\n", st.AvgPnL)
	fmt.Fprintf(&b, "Avg Win: %+.2f USDT\n", st.AvgWin)
	fmt.Fprintf(&b, "Avg Loss: %+.2f USDT\n\n", st.AvgLoss)

	b.WriteString("Причины закрытия:\n")
	fmt.Fprintf(&b, "• TP: %d\n", st.TPCount)
	fmt.Fprintf(&b, "• SL: %d\n", st.SLCount)
	fmt.Fprintf(&b, "• TIME_STOP: %d\n", st.TimeStopCount)
	fmt.Fprintf(&b, "• PARTIAL: %d\n", st.PartialCount)
	fmt.Fprintf(&b, "• MANUAL: %d\n", st.ManualCount)
	fmt.Fprintf(&b, "• UNKNOWN: %d", st.UnknownCount)

	return b.String()
}
