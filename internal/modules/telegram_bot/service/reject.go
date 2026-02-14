package service

//func (t *Telegram) sendRejects(ctx context.Context, chatID int64, reset bool) {
//	// только админ/сервис чат (как тебе удобнее)
//	if t.cfg.ServiceTelegramChatID != 0 && chatID != int64(t.cfg.ServiceTelegramChatID) {
//		_, _ = t.Send(ctx, chatID, "⛔️ Доступно только в админ-чате.")
//		return
//	}
//
//	snap := t.router.StrategyRejects(reset)
//
//	var b strings.Builder
//	b.WriteString("📊 <b>Reject статистика стратегии</b>\n\n")
//
//	b.WriteString(fmt.Sprintf("Период: %s — %s\n",
//		snap.From.Format("15:04:05"),
//		snap.To.Format("15:04:05"),
//	))
//	b.WriteString(fmt.Sprintf("Всего отклонений: <b>%d</b>\n\n", snap.Total))
//
//	if snap.Total == 0 || len(snap.Top) == 0 {
//		b.WriteString("Пока нет данных.\n")
//	} else {
//		b.WriteString("<b>Топ причин:</b>\n")
//		for i, it := range snap.Top {
//			b.WriteString(fmt.Sprintf("%d) <code>%s</code> — %d\n", i+1, it.Reason, it.Count))
//		}
//	}
//
//	if reset {
//		b.WriteString("\n✅ Счётчики сброшены.")
//	}
//
//	msg := tgbot.NewMessage(chatID, b.String())
//	msg.ParseMode = "HTML"
//	_, _ = t.SendMessage(ctx, msg)
//}
