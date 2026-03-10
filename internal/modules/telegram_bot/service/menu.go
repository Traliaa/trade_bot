package service

import (
	"context"
	"fmt"
	"strings"
	"trade_bot/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (t *Telegram) buildSettingsMenu(ctx context.Context, chatID int64) (text string, kb tgbotapi.InlineKeyboardMarkup, err error) {
	session, ok := t.router.GetSession(chatID)
	if !ok {
		t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	modeStr := models.TuneModeString(t.router.TuneMode(ctx))

	ts := session.User.Settings.TradingSettings
	tr := session.User.Settings.TrailingConfig

	var b strings.Builder
	b.WriteString("⚙️ *Настройки торговли*\n\n")

	fmt.Fprintf(&b,
		"💰 *Размер позиции*: `%.2f%%`\n— Сколько депозита используется в сделке\n\n"+
			"⚠️ *Риск*: `%.2f%%`\n— Потеря при срабатывании стопа\n\n"+
			"📉 *Стоп*: `%.2f%%`\n— Допустимое движение против тебя\n\n"+
			"🎯 *Тейк*: `%.2fR`\n— Прибыль относительно риска\n\n"+
			"📊 *Плечо*: `x%d`\n"+
			"🔢 *Макс. позиций*: `%d`\n\n"+
			"↘️ *Частичная фиксация*: *%s* (%.0f%%)\n",
		ts.PositionPct,
		ts.RiskPct,
		ts.StopPct,
		ts.TakeProfitRR,
		ts.Leverage,
		ts.MaxOpenPositions,
		onOff(tr.PartialEnabled),
		tr.PartialCloseFrac*100,
	)

	kb = tgbotapi.NewInlineKeyboardMarkup(
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
			btn("📉 Trailing / Partial", "menu:trailing"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn("✨ Фичи", "menu:features"),
		),
	)

	if chatID == adminChat {
		kb = tgbotapi.NewInlineKeyboardMarkup(
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
				btn("📉 Trailing / Partial", "menu:trailing"),
			),
			tgbotapi.NewInlineKeyboardRow(
				btn("✨ Фичи", "menu:features"),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("📊 Reject статистика", cbAdminRejects),
				tgbotapi.NewInlineKeyboardButtonData("🧹 Сбросить счётчики", cbAdminRejectsReset),
			),
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⚙️ Авто-тюн сейчас", cbAdminTuneNow),
				tgbotapi.NewInlineKeyboardButtonData("🧠 Режим тюнинга: "+modeStr, cbAdminTuneModeToggle),
			),
		)
	}

	return b.String(), kb, nil
}

func (t *Telegram) handleSettingsMenu(ctx context.Context, chatID int64) {
	text, kb, err := t.buildSettingsMenu(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	_, _ = t.SendMessage(ctx, msg)
}

func (t *Telegram) handleSettingsMenuEdit(ctx context.Context, chatID int64, msgID int) {
	text, kb, err := t.buildSettingsMenu(ctx, chatID)
	if err != nil {
		return
	}

	edit := tgbotapi.NewEditMessageText(chatID, msgID, text)
	edit.ParseMode = "Markdown"
	edit.ReplyMarkup = &kb
	_, _ = t.bot.Request(edit)
}
func (t *Telegram) handleTrailingMenu(ctx context.Context, chatID int64) {
	session, ok := t.router.GetSession(chatID)
	if !ok {
		t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	tr := session.User.Settings.TrailingConfig

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
		tr.TimeStopBars, tr.EarlyTimeStopMinMFER,
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

func (t *Telegram) handleFeaturesMenu(ctx context.Context, chatID int64) {

	session, ok := t.router.GetSession(chatID)
	if !ok {
		t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}
	if !session.User.Premium {
		return
	}
	ff := session.User.Settings.FeatureFlags

	var b strings.Builder
	b.WriteString("✨ *Фичи бота*\n\n")

	fmt.Fprintf(&b,
		"🛡 *Защита «почти тейк → стоп выше»*: *%s*\n"+
			"— Если цена была близко к тейку и откатилась,\n"+
			"  бот подтягивает стоп, чтобы не уйти в минус\n\n"+
			"🧪 *Симуляция перед входом*: *%s*\n"+
			"— Сначала покажет расчёты SL/TP/объёма,\n"+
			"  и только потом попросит подтвердить вход\n\n"+
			"📉 *График сделки в Telegram*: *%s*\n"+
			"— После входа/выхода пришлёт мини-график\n\n"+
			"🤖 *Авто-рекомендации*: *%s*\n"+
			"— Подсказки по настройкам на основе результатов\n\n"+
			"💎 *PRO режим*: *%s*\n"+
			"— Показывает расширенные пункты меню\n",
		onOff(ff.NearTPProtectEnabled),
		onOff(ff.SimulateBeforeEntry),
		onOff(ff.DealChartEnabled),
		onOff(ff.AutoRecommendEnabled),
		onOff(ff.ProMode),
	)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn(toggleLabel("🛡 Защита Near-TP", ff.NearTPProtectEnabled), "toggle:feat:near_tp"),
			btn(toggleLabel("🧪 Симуляция", ff.SimulateBeforeEntry), "toggle:feat:simulate"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn(toggleLabel("📉 График", ff.DealChartEnabled), "toggle:feat:chart"),
			btn(toggleLabel("🤖 Рекомендации", ff.AutoRecommendEnabled), "toggle:feat:reco"),
		),
		tgbotapi.NewInlineKeyboardRow(
			btn(toggleLabel("💎 PRO", ff.ProMode), "toggle:feat:pro"),
			btn("⬅️ Назад", "menu:settings"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, b.String())
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb
	_, _ = t.SendMessage(ctx, msg)
}
