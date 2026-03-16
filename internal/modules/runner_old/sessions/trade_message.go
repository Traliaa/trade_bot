package sessions

import (
	"fmt"
	"time"
	"trade_bot/internal/models"
)

func formatClosedTradeMessage(tr models.TradeRecord, closeInput models.TradeCloseInput) string {
	p := closeInput.Payload

	exitAt := closeInput.ExitAt.Format(time.DateTime)

	return fmt.Sprintf(
		"🔒 Сделка закрыта\n\nИнструмент: %s\nСторона: %s\nВход: %.6f\nВыход: %.6f\nРазмер: %.4f\nPnL: %.4f\nPnL %%: %.2f\nR: %.2f\nПричина: %s\nВремя закрытия: %s",
		tr.InstID,
		p.PosSide,
		p.EntryPrice,
		p.ExitPrice,
		p.ExitSize,
		p.RealizedPnL,
		p.RealizedPnLPct,
		p.RMultiple,
		closeInput.CloseReason,
		exitAt,
	)
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
