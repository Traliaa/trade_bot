package service

import (
	"fmt"
	"trade_bot/internal/models"
)

func formatStatsMessage(st models.TradeStats) string {
	return fmt.Sprintf(
		"📊 Статистика\n\n"+
			"Всего сделок: %d\n"+
			"Открытых: %d\n"+
			"Закрытых: %d\n\n"+
			"Побед: %d\n"+
			"Поражений: %d\n"+
			"Безубыток: %d\n"+
			"Winrate: %.2f%%\n\n"+
			"Total PnL: %.4f\n"+
			"Avg PnL: %.4f\n"+
			"Profit Factor: %.2f\n\n"+
			"Total R: %.2f\n"+
			"Avg R: %.2f\n"+
			"Median R: %.2f\n\n"+
			"Avg Duration: %d sec\n"+
			"Avg MFE: %.2fR\n"+
			"Avg MAE: %.2fR\n\n"+
			"TP: %d\n"+
			"SL: %d\n"+
			"BE: %d\n"+
			"Lock Profit: %d\n"+
			"Partial Exit: %d\n"+
			"TimeStop Early: %d\n"+
			"TimeStop Stale: %d\n"+
			"Manual: %d\n"+
			"Recovery: %d\n"+
			"Force Close: %d\n"+
			"Unknown: %d\n\n"+
			"Best Trade: %.2fR\n"+
			"Worst Trade: %.2fR",
		st.TotalTrades,
		st.OpenTrades,
		st.ClosedTrades,

		st.Wins,
		st.Losses,
		st.BreakevenTrades,
		st.WinRate,

		st.TotalPnL,
		st.AvgPnL,
		st.ProfitFactor,

		st.TotalR,
		st.AvgR,
		st.MedianR,

		st.AvgDurationSec,
		st.AvgMFER,
		st.AvgMAER,

		st.TPCount,
		st.SLCount,
		st.BreakEvenCount,
		st.LockProfitCount,
		st.PartialExitCount,
		st.TimeStopEarlyCount,
		st.TimeStopStaleCount,
		st.ManualCloseCount,
		st.RecoveryCloseCount,
		st.ForceCloseCount,
		st.UnknownCloseCount,

		st.BestTradeR,
		st.WorstTradeR,
	)
}
