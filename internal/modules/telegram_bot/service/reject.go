package service

import (
	"context"
	"fmt"
	"strings"
	"trade_bot/internal/modules/strategy/service"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const adminChat = 213532199

func (t *Telegram) sendRejects(ctx context.Context, chatID int64, reset bool) {
	// только админ-чат (сервисный)
	if chatID != adminChat {
		_, _ = t.Send(ctx, chatID, "⛔️ Доступно только в админ-чате.")
		return
	}

	// Router интерфейсный — вытаскиваем capability через type assert
	type rejectsProvider interface {
		StrategyRejects(reset bool) service.RejectSnapshot
	}

	rp, ok := any(t.router).(rejectsProvider)
	if !ok {
		_, _ = t.Send(ctx, chatID, "⚠️ Reject статистика недоступна (router не поддерживает StrategyRejects).")
		return
	}

	snap := rp.StrategyRejects(reset)

	var b strings.Builder
	b.WriteString("📊 <b>Reject статистика стратегии</b>\n\n")

	// Период
	b.WriteString(fmt.Sprintf("Период: %s — %s\n",
		snap.From.Format("15:04:05"),
		snap.To.Format("15:04:05"),
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

	msg := tgbot.NewMessage(chatID, b.String())
	msg.ParseMode = "HTML"
	_, _ = t.SendMessage(ctx, msg)
}
