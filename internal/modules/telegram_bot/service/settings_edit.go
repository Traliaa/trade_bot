package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"trade_bot/internal/models"
)

func (t *Telegram) askValue(ctx context.Context, chatID int64, key string) {
	t.setAwait(chatID, key)

	var hint string
	switch key {
	// --- TradingSettings ---
	case "position":
		hint = "Введи *размер позиции* в %, например: `1.0`"
	case "risk":
		hint = "Введи *риск* в %, например: `1.0` (1% риска по стопу)"
	case "stop":
		hint = "Введи *стоп* в %, например: `1.2`"
	case "tp_rr":
		hint = "Введи *тейк* в R, например: `2.0` (TP=2R)"
	case "lev":
		hint = "Введи *плечо* (целое), например: `10`"
	case "maxpos":
		hint = "Введи *макс. открытых позиций* (целое), например: `6`"

	// --- TrailingConfig (ВСЕ поля) ---
	case "be_trigger_r":
		hint = "Введи *BE Trigger* в R, например: `0.6`"
	case "be_offset_r":
		hint = "Введи *BE Offset* в R, например: `0.0`"
	case "lock_trigger_r":
		hint = "Введи *Lock Trigger* в R, например: `0.9`"
	case "lock_offset_r":
		hint = "Введи *Lock Offset* в R, например: `0.3`"
	case "timestop_bars":
		hint = "Введи *TimeStop Bars* (целое), например: `12`"
	case "timestop_min_mfe_r":
		hint = "Введи *TimeStop Min MFE* в R, например: `0.3`"
	case "partial_trigger_r":
		hint = "Введи *Partial Trigger* в R, например: `0.9`"
	case "partial_close_frac":
		hint = "Введи *Partial Close* в % (1..100), например: `50`"

	default:
		hint = "Введи значение"
	}

	_, _ = t.Send(ctx, chatID, "✍️ "+hint+"\n\nОтмена: напиши `отмена`")
}

func (t *Telegram) handleAwaitValue(ctx context.Context, chatID int64, text, key string) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	text = strings.TrimSpace(text)
	if strings.EqualFold(text, "отмена") {
		t.clearAwait(chatID)
		t.handleSettingsMenu(ctx, chatID)
		return
	}
	text = strings.ReplaceAll(text, ",", ".")

	ts := &user.Settings.TradingSettings
	tr := &user.Settings.TrailingConfig

	switch key {
	// -------- TradingSettings --------
	case "position":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 50 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..50, например `1.0`")
			return
		}
		ts.PositionPct = v

	case "risk":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 10 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..10, например `1.0`")
			return
		}
		ts.RiskPct = v

	case "stop":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 50 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..50, например `1.2`")
			return
		}
		ts.StopPct = v

	case "tp_rr":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `2.0`")
			return
		}
		ts.TakeProfitRR = v

	case "lev":
		v, err := strconv.Atoi(text)
		if err != nil || v < 1 || v > 125 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно целое 1..125, например `10`")
			return
		}
		ts.Leverage = v

	case "maxpos":
		v, err := strconv.Atoi(text)
		if err != nil || v < 1 || v > 50 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно целое 1..50, например `6`")
			return
		}
		ts.MaxOpenPositions = v

	// -------- TrailingConfig (ВСЕ поля) --------
	case "be_trigger_r":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `0.6`")
			return
		}
		tr.BETriggerR = v

	case "be_offset_r":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v < 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `0.0`")
			return
		}
		tr.BEOffsetR = v

	case "lock_trigger_r":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `0.9`")
			return
		}
		tr.LockTriggerR = v

	case "lock_offset_r":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v < 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `0.3`")
			return
		}
		tr.LockOffsetR = v

	case "timestop_bars":
		v, err := strconv.Atoi(text)
		if err != nil || v < 1 || v > 1000 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно целое 1..1000, например `12`")
			return
		}
		tr.TimeStopBars = v

	case "timestop_min_mfe_r":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v < 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `0.3`")
			return
		}
		tr.TimeStopMinMFER = v

	case "partial_trigger_r":
		v, err := strconv.ParseFloat(text, 64)
		if err != nil || v <= 0 || v > 20 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..20, например `0.9`")
			return
		}
		tr.PartialTriggerR = v

	case "partial_close_frac":
		// ввод в процентах
		p, err := strconv.ParseFloat(text, 64)
		if err != nil || p <= 0 || p > 100 {
			_, _ = t.Send(ctx, chatID, "❗️Нужно число 0..100, например `50`")
			return
		}
		tr.PartialCloseFrac = p / 100.0

	default:
		_, _ = t.Send(ctx, chatID, "❗️Неизвестная настройка")
		return
	}

	// sanity: если ConfirmTimeout пустой, подставим дефолт
	if ts.ConfirmTimeout <= 0 {
		ts.ConfirmTimeout = 30 * time.Second
	}

	if err := t.repo.Update(ctx, user); err != nil {
		_, _ = t.Send(ctx, chatID, "⚠️ Не удалось сохранить настройку: "+err.Error())
		return
	}

	// ✅ успех — чистим await
	t.popAwait(chatID)

	_, _ = t.Send(ctx, chatID, "✅ Сохранено")

	// куда возвращать — по ключу
	if isTrailingKey(key) {
		t.handleTrailingMenu(ctx, chatID)
		return
	}
	t.handleSettingsMenu(ctx, chatID)
}

func isTrailingKey(key string) bool {
	switch key {
	case "be_trigger_r", "be_offset_r", "lock_trigger_r", "lock_offset_r",
		"timestop_bars", "timestop_min_mfe_r", "partial_trigger_r", "partial_close_frac":
		return true
	default:
		return false
	}
}

// применить пресет
func (t *Telegram) applyPreset(ctx context.Context, chatID int64, key string) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	p, ok := models.Presets[key]
	if !ok {
		_, _ = t.Send(ctx, chatID, "Неизвестный пресет")
		return
	}

	p.Apply(&user.Settings.TradingSettings, &user.Settings.TrailingConfig)
	_ = t.repo.Update(ctx, user)

	_, _ = t.Send(ctx, chatID, fmt.Sprintf("✅ Применён пресет: *%s*\n%s", p.Name, p.Description))
	t.handleSettingsMenu(ctx, chatID)
}
