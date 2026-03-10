package service

import "context"

func (t *Telegram) handleStats(ctx context.Context, chatID int64) {
	stats, err := t.router.GetTradeStats(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "❌ Не удалось загрузить статистику сделок.")
		return
	}

	_, _ = t.Send(ctx, chatID, formatStatsMessage(stats))
}
