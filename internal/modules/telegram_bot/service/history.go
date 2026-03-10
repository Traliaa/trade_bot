package service

import (
	"context"
)

func (t *Telegram) handleHistory(ctx context.Context, chatID int64) {
	const limit = 10

	trades, err := t.router.ListRecentTrades(ctx, chatID, limit)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "❌ Не удалось загрузить историю сделок.")
		return
	}

	// если в router возвращается nil,nil
	if len(trades) == 0 {
		_, _ = t.Send(ctx, chatID, "📚 История сделок пока пустая.")
		return
	}

	_, _ = t.Send(ctx, chatID, formatHistoryMessage(trades))
}
