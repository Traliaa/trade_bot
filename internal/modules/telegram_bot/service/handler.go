package service

import (
	"context"
	"fmt"
	"strings"

	"trade_bot/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (t *Telegram) handleCallback(ctx context.Context, chatID int64, cb *tgbotapi.CallbackQuery) {
	// убрать "часики"
	_, _ = t.bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	//user, err := t.getUser(ctx, chatID)
	//if err != nil {
	//	_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
	//	return
	//}

	data := cb.Data

	switch data {
	case "toggle:confirm":
		t.toggleConfirm(ctx, chatID)
		return
	case "toggle:partial":
		t.togglePartial(ctx, chatID)
		return
	}

	if strings.HasPrefix(data, "preset:") {
		key := strings.TrimPrefix(data, "preset:")
		t.applyPreset(ctx, chatID, key)
		return
	}
	if strings.HasPrefix(data, "tr_preset:") {
		key := strings.TrimPrefix(data, "tr_preset:")

		preset, ok := models.TrailingPresets[key]
		if !ok {
			_, _ = t.Send(ctx, chatID, "Неизвестный трейлинг-пресет")
			return
		}

		user, err := t.getUser(ctx, chatID)
		if err != nil {
			_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
			return
		}

		preset.Apply(&user.Settings.TrailingConfig)

		if err := t.repo.Update(ctx, user); err != nil {
			_, _ = t.Send(ctx, chatID, "⚠️ Не удалось применить пресет")
			return
		}

		_, _ = t.Send(ctx, chatID,
			fmt.Sprintf("✅ Применён пресет:\n*%s*\n_%s_",
				preset.Name, preset.Description,
			),
		)

		t.handleTrailingMenu(ctx, chatID)
		return
	}

	if strings.HasPrefix(data, "set:") {
		key := strings.TrimPrefix(data, "set:")
		t.askValue(ctx, chatID, key)
		return
	}
	switch data {
	case "menu:trailing":
		t.handleTrailingMenu(ctx, chatID)
		return
	case "menu:settings":
		t.handleSettingsMenu(ctx, chatID)
		return
	}

}
func (t *Telegram) handleSettingsMenu(ctx context.Context, chatID int64) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	ts := user.Settings.TradingSettings
	tr := user.Settings.TrailingConfig

	var b strings.Builder
	b.WriteString("⚙️ *Настройки торговли*\n\n")

	fmt.Fprintf(&b,
		"💰 *Размер позиции*: `%.2f%%`\n— Сколько депозита используется в сделке\n\n"+
			"⚠️ *Риск*: `%.2f%%`\n— Потеря при срабатывании стопа\n\n"+
			"📉 *Стоп*: `%.2f%%`\n— Допустимое движение против тебя\n\n"+
			"🎯 *Тейк*: `%.2fR`\n— Прибыль относительно риска\n\n"+
			"📊 *Плечо*: `x%d`\n"+
			"🔢 *Макс. позиций*: `%d`\n\n"+
			"🔔 *Подтверждение входа*: *%s*\n"+
			"↘️ *Частичная фиксация*: *%s* (%.0f%%)\n",
		ts.PositionPct,
		ts.RiskPct,
		ts.StopPct,
		ts.TakeProfitRR,
		ts.Leverage,
		ts.MaxOpenPositions,
		onOff(ts.ConfirmRequired),
		onOff(tr.PartialEnabled),
		tr.PartialCloseFrac*100,
	)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("🟢 Консервативный", "preset:safe"),
			btn("🟡 Средний", "preset:mid"),
			btn("🔴 Агрессивный", "preset:aggr"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("💰 Размер позиции", "set:position"),
			btn("⚠️ Риск", "set:risk"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("📉 Стоп %", "set:stop"),
			btn("🎯 Тейк R", "set:tp_rr"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("📊 Плечо", "set:lev"),
			btn("🔢 Макс позиций", "set:maxpos"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("🔔 Подтверждение", "toggle:confirm"),
			btn("📉 Trailing / Partial", "menu:trailing"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, b.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	_, _ = t.SendMessage(ctx, msg)
}
func (t *Telegram) handleTrailingMenu(ctx context.Context, chatID int64) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	tr := user.Settings.TrailingConfig

	var b strings.Builder
	b.WriteString("📉 *Trailing / Partial*\n\n")

	fmt.Fprintf(&b,
		"🟢 *Безубыток (BE)*\n"+
			"• Условие: `%.2fR`\n"+
			"• Сдвиг стопа: `%.2fR`\n"+
			"— При достижении указанной прибыли\n"+
			"  стоп-лосс переносится в точку входа\n"+
			"  или в небольшой плюс\n\n"+
			"🔒 *Фиксация прибыли (Lock)*\n"+
			"• Условие: `%.2fR`\n"+
			"• Фиксация: `+%.2fR`\n"+
			"— При росте цены стоп-лосс подтягивается выше,\n"+
			"  чтобы сохранить часть заработанной прибыли\n\n"+
			"⏱ *Выход по времени (TimeStop)*\n"+
			"• Ожидание: `%d` свечей\n"+
			"• Минимальный прогресс: `%.2fR`\n"+
			"— Если за это время цена почти не движется,\n"+
			"  сделка закрывается как неэффективная\n\n"+
			"↘️ *Частичная фиксация*: *%s*\n"+
			"• Условие: `%.2fR`\n"+
			"• Закрыть: `%.0f%%` позиции\n"+
			"— Часть позиции фиксируется,\n"+
			"  остальное остаётся на дальнейший рост\n\n"+
			"💡 R — это отношение прибыли к риску (1R = риск по стоп-лоссу)",
		tr.BETriggerR, tr.BEOffsetR,
		tr.LockTriggerR, tr.LockOffsetR,
		tr.TimeStopBars, tr.TimeStopMinMFER,
		onOff(tr.PartialEnabled),
		tr.PartialTriggerR,
		tr.PartialCloseFrac*100,
	)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("🟢 Осторожный", "tr_preset:safe"),
			btn("🟡 Сбаланс.", "tr_preset:mid"),
			btn("🔴 Агрессивный", "tr_preset:aggr"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("🟢 BE Trigger", "set:be_trigger_r"),
			btn("🟢 BE Offset", "set:be_offset_r"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("🔒 Lock Trigger", "set:lock_trigger_r"),
			btn("🔒 Lock Offset", "set:lock_offset_r"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("⏱ Bars", "set:timestop_bars"),
			btn("⏱ Min MFE", "set:timestop_min_mfe_r"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("↘️ Partial ON/OFF", "toggle:partial"),
			btn("↘️ Trigger", "set:partial_trigger_r"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("↘️ Close %", "set:partial_close_frac"),
			btn("⬅️ Назад", "menu:settings"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, b.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	_, _ = t.SendMessage(ctx, msg)
}
