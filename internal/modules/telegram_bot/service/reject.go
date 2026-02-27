package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const adminChat = 213532199

func (t *Telegram) sendRejects(ctx context.Context, chatID int64, reset bool) {
	// только админ-чат (сервисный)
	if chatID != adminChat {
		_, _ = t.Send(ctx, chatID, "⛔️ Доступно только в админ-чате.")
		return
	}

	snap := t.router.StrategyRejects(reset)

	var b strings.Builder
	b.WriteString("📊 <b>Reject статистика стратегии</b>\n\n")

	// Период
	b.WriteString(fmt.Sprintf("Период: %s — %s\n",
		snap.From.Add(time.Hour*3).Format("15:04:05"),
		snap.To.Add(time.Hour*3).Format("15:04:05"),
	))

	// Total (uint64)
	b.WriteString(fmt.Sprintf("Всего отклонений: <b>%d</b>\n\n", snap.Total))

	if snap.Total == 0 || len(snap.Top) == 0 {
		b.WriteString("Пока нет данных.\n")
	} else {
		b.WriteString("<b>Топ причин:</b>\n")
		for i, it := range snap.Top {
			b.WriteString(fmt.Sprintf("%d) <code>%s</code> — %d\n", i+1, it.Reason, it.Count))
		}
	}

	if reset {
		b.WriteString("\n✅ Счётчики сброшены.")
	}

	tu, lastSig, lastTune := t.router.StrategyTuning()

	b.WriteString("\n<b>Текущие пороги:</b>\n")
	b.WriteString(fmt.Sprintf("• CloseUpMin: <code>%.2f</code>\n", tu.CloseUpMin))
	b.WriteString(fmt.Sprintf("• CloseDnMax: <code>%.2f</code>\n", tu.CloseDnMax))
	b.WriteString(fmt.Sprintf("• MinBodyPct: <code>%.4f</code>\n", tu.MinBodyPct))
	b.WriteString(fmt.Sprintf("• MinChannelPct: <code>%.4f</code>\n", tu.MinChannelPct))
	b.WriteString(fmt.Sprintf("• BreakoutPct: <code>%.4f</code>\n", tu.BreakoutPct))
	if !lastSig.IsZero() {
		b.WriteString(fmt.Sprintf("Последний сигнал: <code>%s</code>\n", lastSig.Add(time.Hour*3).Format("15:04:05")))
	} else {
		b.WriteString("Последний сигнал: <code>нет</code>\n")
	}
	if !lastTune.IsZero() {
		b.WriteString(fmt.Sprintf("Последний авто-тюн: <code>%s</code>\n", lastTune.Add(time.Hour*3).Format("15:04:05")))
	} else {
		b.WriteString("Последний авто-тюн: <code>нет</code>\n")
	}

	msg := tgbot.NewMessage(chatID, b.String())
	msg.ParseMode = "HTML"
	_, _ = t.SendMessage(ctx, msg)
}
