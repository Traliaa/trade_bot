package service

import (
	"context"
	"strconv"
	"strings"
	"time"
	"trade_bot/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func mainSettingsKB() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Торговля", "settings:trade"),
			tgbotapi.NewInlineKeyboardButtonData("📉 Риск/SL/TP", "settings:risk"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧲 Trailing", "settings:trail"),
		),
	)
}
func tradeSettingsKB(ts *models.TradingSettings) tgbotapi.InlineKeyboardMarkup {
	confirmBtn := "⭕️ Confirm: выкл"
	if ts.ConfirmRequired {
		confirmBtn = "✅ Confirm: вкл"
	}

	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Lev −1", "trade:lev:-1"),
			tgbotapi.NewInlineKeyboardButtonData("Lev +1", "trade:lev:+1"),
			tgbotapi.NewInlineKeyboardButtonData("Lev +5", "trade:lev:+5"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("MaxPos −1", "trade:maxpos:-1"),
			tgbotapi.NewInlineKeyboardButtonData("MaxPos +1", "trade:maxpos:+1"),
			tgbotapi.NewInlineKeyboardButtonData("MaxPos +5", "trade:maxpos:+5"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Pos% 0.5", "trade:pospct:set:0.5"),
			tgbotapi.NewInlineKeyboardButtonData("Pos% 1.0", "trade:pospct:set:1.0"),
			tgbotapi.NewInlineKeyboardButtonData("Pos% 2.0", "trade:pospct:set:2.0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✍️ Pos% вручную", "trade:pospct:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(confirmBtn, "trade:toggle_confirm"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏱ Timeout", "trade:timeout:ask"),
			tgbotapi.NewInlineKeyboardButtonData("🕒 Cooldown", "trade:cooldown:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "settings:back"),
		),
	)
}

func (t *Telegram) handleCallback(ctx context.Context, chatID int64, cb *tgbotapi.CallbackQuery) {
	// убрать "часики"
	_, _ = t.bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	data := cb.Data

	switch {
	// --- навигация ---
	case data == "settings:trade":
		t.renderTradeSettings(ctx, chatID, cb.Message.MessageID, user)
		return
	case data == "settings:risk":
		t.renderRiskSettings(ctx, chatID, cb.Message.MessageID, user)
		return
	case data == "settings:trail":
		t.renderTrailingSettings(ctx, chatID, cb.Message.MessageID, user)
		return
	case data == "settings:back":
		t.editTextAndKb(ctx, chatID, cb.Message.MessageID, "*Настройки бота*", mainSettingsKB())
		return

	// --- trade ---
	case strings.HasPrefix(data, "trade:"):
		t.handleTradeCb(ctx, chatID, cb.Message, user, data)
		return

	// --- risk ---
	case strings.HasPrefix(data, "risk:"):
		t.handleRiskCb(ctx, chatID, cb.Message, user, data)
		return

	// --- trail ---
	case strings.HasPrefix(data, "trail:"):
		t.handleTrailCb(ctx, chatID, cb.Message, user, data)
		return
	}
}

func (t *Telegram) handleTradeCb(ctx context.Context, chatID int64, msg *tgbotapi.Message, user *models.UserSettings, data string) {
	ts := &user.Settings.TradingSettings

	switch {
	case data == "trade:toggle_confirm":
		ts.ConfirmRequired = !ts.ConfirmRequired
		_ = t.repo.Update(ctx, user)
		t.renderTradeSettings(ctx, chatID, msg.MessageID, user)
		return

	case strings.HasPrefix(data, "trade:lev:"):
		delta := mustInt(strings.TrimPrefix(data, "trade:lev:"))
		ts.Leverage += delta
		if ts.Leverage < 1 {
			ts.Leverage = 1
		}
		if ts.Leverage > 125 {
			ts.Leverage = 125
		}
		_ = t.repo.Update(ctx, user)
		t.renderTradeSettings(ctx, chatID, msg.MessageID, user)
		return

	case strings.HasPrefix(data, "trade:maxpos:"):
		delta := mustInt(strings.TrimPrefix(data, "trade:maxpos:"))
		ts.MaxOpenPositions += delta
		if ts.MaxOpenPositions < 1 {
			ts.MaxOpenPositions = 1
		}
		if ts.MaxOpenPositions > 50 {
			ts.MaxOpenPositions = 50
		}
		_ = t.repo.Update(ctx, user)
		t.renderTradeSettings(ctx, chatID, msg.MessageID, user)
		return

	case strings.HasPrefix(data, "trade:pospct:set:"):
		v := mustFloat(strings.TrimPrefix(data, "trade:pospct:set:"))
		if v <= 0 || v > 100 {
			return
		}
		ts.PositionPct = v
		_ = t.repo.Update(ctx, user)
		t.renderTradeSettings(ctx, chatID, msg.MessageID, user)
		return

	case data == "trade:pospct:ask":
		t.ask(ctx, chatID, "Введи PositionPct (например `1.0` = 1%):", "await:trade:pospct")
		return

	case data == "trade:timeout:ask":
		t.ask(ctx, chatID, "Введи ConfirmTimeout (например `30s`, `2m`):", "await:trade:timeout")
		return

	case data == "trade:cooldown:ask":
		t.ask(ctx, chatID, "Введи CooldownPerSymbol (например `30m`, `6h`):", "await:trade:cooldown")
		return
	}
}
func (t *Telegram) handleRiskCb(ctx context.Context, chatID int64, msg *tgbotapi.Message, user *models.UserSettings, data string) {
	ts := &user.Settings.TradingSettings

	switch {
	case strings.HasPrefix(data, "risk:riskpct:set:"):
		v := mustFloat(strings.TrimPrefix(data, "risk:riskpct:set:"))
		if v <= 0 || v > 10 {
			return
		}
		ts.RiskPct = v
		_ = t.repo.Update(ctx, user)
		t.renderRiskSettings(ctx, chatID, msg.MessageID, user)
		return

	case data == "risk:riskpct:ask":
		t.ask(ctx, chatID, "Введи RiskPct (например `0.5`):", "await:risk:riskpct")
		return

	case strings.HasPrefix(data, "risk:stoppct:set:"):
		v := mustFloat(strings.TrimPrefix(data, "risk:stoppct:set:"))
		if v <= 0 || v > 20 {
			return
		}
		ts.StopPct = v
		_ = t.repo.Update(ctx, user)
		t.renderRiskSettings(ctx, chatID, msg.MessageID, user)
		return

	case data == "risk:stoppct:ask":
		t.ask(ctx, chatID, "Введи StopPct (например `1.2`):", "await:risk:stoppct")
		return

	case strings.HasPrefix(data, "risk:tp:set:"):
		v := mustFloat(strings.TrimPrefix(data, "risk:tp:set:"))
		if v < 0.5 || v > 10 {
			return
		}
		ts.TakeProfitRR = v
		_ = t.repo.Update(ctx, user)
		t.renderRiskSettings(ctx, chatID, msg.MessageID, user)
		return

	case data == "risk:tp:ask":
		t.ask(ctx, chatID, "Введи TakeProfitRR (например `2.0`):", "await:risk:tp")
		return
	}
}
func (t *Telegram) handleTrailCb(ctx context.Context, chatID int64, msg *tgbotapi.Message, user *models.UserSettings, data string) {
	cfg := &user.Settings.TrailingConfig

	switch data {
	case "trail:toggle_partial":
		cfg.PartialEnabled = !cfg.PartialEnabled
		_ = t.repo.Update(ctx, user)
		t.renderTrailingSettings(ctx, chatID, msg.MessageID, user)
		return

	case "trail:be_trg:ask":
		t.ask(ctx, chatID, "Введи BETriggerR (например `0.6`):", "await:trail:be_trg")
		return
	case "trail:be_off:ask":
		t.ask(ctx, chatID, "Введи BEOffsetR (например `0.0`):", "await:trail:be_off")
		return
	case "trail:lock_trg:ask":
		t.ask(ctx, chatID, "Введи LockTriggerR (например `0.9`):", "await:trail:lock_trg")
		return
	case "trail:lock_off:ask":
		t.ask(ctx, chatID, "Введи LockOffsetR (например `0.3`):", "await:trail:lock_off")
		return
	case "trail:time_bars:ask":
		t.ask(ctx, chatID, "Введи TimeStopBars (например `12`):", "await:trail:time_bars")
		return
	case "trail:minmfe:ask":
		t.ask(ctx, chatID, "Введи TimeStopMinMFER (например `0.3`):", "await:trail:minmfe")
		return
	case "trail:partial_trg:ask":
		t.ask(ctx, chatID, "Введи PartialTriggerR (например `0.9`):", "await:trail:partial_trg")
		return
	case "trail:partial_close:ask":
		t.ask(ctx, chatID, "Введи PartialCloseFrac в % (например `50`):", "await:trail:partial_close")
		return
	}
}

func (t *Telegram) handleSettingsMenu(ctx context.Context, chatID int64) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚙️ Торговля", "settings:trade"),
			tgbotapi.NewInlineKeyboardButtonData("📉 Риск / SL / TP", "settings:risk"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🧲 Trailing / Partial", "settings:trail"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, "*Настройки бота*")
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb

	_, _ = t.SendMessage(ctx, msg)
}

func riskSettingsKB() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Risk 0.25%", "risk:riskpct:set:0.25"),
			tgbotapi.NewInlineKeyboardButtonData("Risk 0.5%", "risk:riskpct:set:0.5"),
			tgbotapi.NewInlineKeyboardButtonData("Risk 1%", "risk:riskpct:set:1.0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✍️ Risk вручную", "risk:riskpct:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Stop 0.7%", "risk:stoppct:set:0.7"),
			tgbotapi.NewInlineKeyboardButtonData("Stop 1.2%", "risk:stoppct:set:1.2"),
			tgbotapi.NewInlineKeyboardButtonData("Stop 2.0%", "risk:stoppct:set:2.0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✍️ Stop вручную", "risk:stoppct:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("TP 1.5R", "risk:tp:set:1.5"),
			tgbotapi.NewInlineKeyboardButtonData("TP 2R", "risk:tp:set:2.0"),
			tgbotapi.NewInlineKeyboardButtonData("TP 3R", "risk:tp:set:3.0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✍️ TP вручную", "risk:tp:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "settings:back"),
		),
	)
}

func trailingKB(cfg *models.TrailingConfig) tgbotapi.InlineKeyboardMarkup {
	partialBtn := "⭕️ Partial: выкл"
	if cfg.PartialEnabled {
		partialBtn = "✅ Partial: вкл"
	}

	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("BE Trg", "trail:be_trg:ask"),
			tgbotapi.NewInlineKeyboardButtonData("BE Off", "trail:be_off:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Lock Trg", "trail:lock_trg:ask"),
			tgbotapi.NewInlineKeyboardButtonData("Lock Off", "trail:lock_off:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Time Bars", "trail:time_bars:ask"),
			tgbotapi.NewInlineKeyboardButtonData("MinMFE", "trail:minmfe:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(partialBtn, "trail:toggle_partial"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Partial Trg", "trail:partial_trg:ask"),
			tgbotapi.NewInlineKeyboardButtonData("Close %", "trail:partial_close:ask"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "settings:back"),
		),
	)
}

func onOffEmoji(v bool) string {
	if v {
		return "✅ вкл"
	}
	return "❌ выкл"
}
func (t *Telegram) handleAwaitValue(ctx context.Context, chatID int64, text string, key string) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "⚠️ Настройки не найдены, попробуй /start")
		return
	}

	ts := &user.Settings.TradingSettings
	tr := &user.Settings.TrailingConfig

	switch key {

	// --- trade ---
	case "await:trade:pospct":
		v, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
		if err != nil || v <= 0 || v > 100 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `1.0` (это 1%)")
			return
		}
		ts.PositionPct = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ PositionPct сохранён")
		return

	case "await:trade:timeout":
		d, err := time.ParseDuration(strings.TrimSpace(text))
		if err != nil || d < 5*time.Second || d > 10*time.Minute {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `30s`, `2m`")
			return
		}
		ts.ConfirmTimeout = d
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ ConfirmTimeout сохранён")
		return

	case "await:trade:cooldown":
		d, err := time.ParseDuration(strings.TrimSpace(text))
		if err != nil || d < 0 || d > 7*24*time.Hour {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `30m`, `6h`")
			return
		}
		ts.CooldownPerSymbol = d
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ CooldownPerSymbol сохранён")
		return

	// --- risk ---
	case "await:risk:riskpct":
		v, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
		if err != nil || v <= 0 || v > 10 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.5` (0.5%)")
			return
		}
		ts.RiskPct = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ RiskPct сохранён")
		return

	case "await:risk:stoppct":
		v, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
		if err != nil || v <= 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `1.2`")
			return
		}
		ts.StopPct = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ StopPct сохранён")
		return

	case "await:risk:tp":
		v, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", "."), 64)
		if err != nil || v < 0.5 || v > 10 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `2.0`")
			return
		}
		ts.TakeProfitRR = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ TakeProfitRR сохранён")
		return

	// --- trailing ---
	case "await:trail:be_trg":
		v := mustFloat(text)
		if v < 0.05 || v > 5 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.6`")
			return
		}
		tr.BETriggerR = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ BETriggerR сохранён")
		return

	case "await:trail:be_off":
		v := mustFloat(text)
		if v < -1 || v > 5 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.0`")
			return
		}
		tr.BEOffsetR = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ BEOffsetR сохранён")
		return

	case "await:trail:lock_trg":
		v := mustFloat(text)
		if v < 0.05 || v > 10 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.9`")
			return
		}
		tr.LockTriggerR = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ LockTriggerR сохранён")
		return

	case "await:trail:lock_off":
		v := mustFloat(text)
		if v < -1 || v > 10 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.3`")
			return
		}
		tr.LockOffsetR = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ LockOffsetR сохранён")
		return

	case "await:trail:time_bars":
		v, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || v < 1 || v > 500 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `12`")
			return
		}
		tr.TimeStopBars = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ TimeStopBars сохранён")
		return

	case "await:trail:minmfe":
		v := mustFloat(text)
		if v < 0 || v > 10 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.3`")
			return
		}
		tr.TimeStopMinMFER = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ TimeStopMinMFER сохранён")
		return

	case "await:trail:partial_trg":
		v := mustFloat(text)
		if v < 0.05 || v > 10 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `0.9`")
			return
		}
		tr.PartialTriggerR = v
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ PartialTriggerR сохранён")
		return

	case "await:trail:partial_close":
		v := mustFloat(text)
		if v <= 0 || v > 100 {
			_, _ = t.Send(ctx, chatID, "Некорректно. Пример: `50` (это 50%)")
			return
		}
		tr.PartialCloseFrac = v / 100.0
		_ = t.repo.Update(ctx, user)
		_, _ = t.Send(ctx, chatID, "✅ PartialCloseFrac сохранён")
		return
	}
}
func (t *Telegram) editTextAndKb(ctx context.Context, chatID int64, msgID int, text string, kb tgbotapi.InlineKeyboardMarkup) {
	edit := tgbotapi.NewEditMessageTextAndMarkup(chatID, msgID, text, kb)
	edit.ParseMode = "Markdown"
	_, _ = t.bot.Send(edit)
}

func (t *Telegram) renderTradeSettings(ctx context.Context, chatID int64, msgID int, user *models.UserSettings) {
	ts := &user.Settings.TradingSettings
	t.editTextAndKb(ctx, chatID, msgID, formatTradeSettings(ts), tradeSettingsKB(ts))
}

func (t *Telegram) renderRiskSettings(ctx context.Context, chatID int64, msgID int, user *models.UserSettings) {
	ts := &user.Settings.TradingSettings
	t.editTextAndKb(ctx, chatID, msgID, formatRiskSettings(ts), riskSettingsKB())
}

func (t *Telegram) renderTrailingSettings(ctx context.Context, chatID int64, msgID int, user *models.UserSettings) {
	cfg := &user.Settings.TrailingConfig
	t.editTextAndKb(ctx, chatID, msgID, formatTrailing(cfg), trailingKB(cfg))
}
