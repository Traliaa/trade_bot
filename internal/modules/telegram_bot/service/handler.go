package service

import (
	"context"
	"fmt"
	"strings"

	"trade_bot/internal/models"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	cbAdminTuneNow        = "admin:tune:now"
	cbAdminTuneModeToggle = "admin:tune:mode:toggle"
)

func (t *Telegram) handleCallback(ctx context.Context, chatID int64, cb *tgbotapi.CallbackQuery) {
	// убрать "часики"
	_, _ = t.bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	data := cb.Data

	switch data {

	case cbAdminRejects:
		t.sendRejects(ctx, chatID, false)
	case cbAdminRejectsReset:
		t.sendRejects(ctx, chatID, true)

	case "toggle:partial":
		t.togglePartial(ctx, chatID)
		return
	case "toggle:feat:near_tp":
		t.toggleFeature(ctx, chatID, "near_tp", cb)
		return
	case "toggle:feat:simulate":
		t.toggleFeature(ctx, chatID, "simulate", cb)
		return
	case "toggle:feat:chart":
		t.toggleFeature(ctx, chatID, "chart", cb)
		return
	case "toggle:feat:reco":
		t.toggleFeature(ctx, chatID, "reco", cb)
		return
	case "toggle:feat:pro":
		t.toggleFeature(ctx, chatID, "pro", cb)
		return
	case "testtrade:open":
		t.openTestTradeBTC1x(ctx, chatID) // реализацию подключим к твоей торговой функции
		return
	case "testtrade:cancel":
		_, _ = t.Send(ctx, chatID, "Ок, отменил ✅")
		return
	case "admin:tune:now":
		// 1) получаем результат тюна
		dec, cur, lastSignalAt, lastTuneAt, warmupDone, mode := t.router.AutoTuneNow(ctx)

		report := FormatTuneDecision(dec, warmupDone, lastSignalAt, lastTuneAt, cur, mode)

		_, _ = t.Send(ctx, chatID, report)                          // отдельное сообщение
		t.handleSettingsMenuEdit(ctx, chatID, cb.Message.MessageID) // обновить кнопку режима
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

		session, ok := t.router.GetSession(chatID)
		if !ok {
			t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
			return
		}

		preset.Apply(&session.User.Settings.TrailingConfig)

		t.router.ApplySettings(ctx, session.User) // ✅ горячее применение
		t.handleSettingsMenu(ctx, chatID)

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
	case "menu:features":
		t.handleFeaturesMenu(ctx, chatID)
		return
	}

}

func toggleLabel(title string, enabled bool) string {
	if enabled {
		return "✅ " + title
	}
	return "⭕️ " + title
}

func (t *Telegram) handleHelp(ctx context.Context, chatID int64) {
	msg := tgbotapi.NewMessage(chatID,
		"❓ *Помощь*\n\n"+
			"Канал поддержки и обновлений:\n"+
			"t.me/trade_bot_info",
	)
	msg.ParseMode = "Markdown"
	_, _ = t.SendMessage(ctx, msg)
}

func (t *Telegram) handleTestTradeMenu(ctx context.Context, chatID int64, user *models.UserSettings) {
	msg := tgbotapi.NewMessage(chatID,
		"🧪 *Тестовая сделка*\n\n"+
			"Открою тестовую сделку по *BTC-USDT-SWAP* с плечом *x1*.\n"+
			"Рекомендуется для проверки ключей и работы ордеров.\n\n"+
			"Продолжить?",
	)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			btn("✅ Открыть тест", "testtrade:open"),
			btn("❌ Отмена", "testtrade:cancel"),
		),
	)
	_, _ = t.SendMessage(ctx, msg)
}
