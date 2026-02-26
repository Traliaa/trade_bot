package service

import (
	"context"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (t *Telegram) togglePartial(ctx context.Context, chatID int64) {
	session, err := t.router.GetSession(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	session.User.Settings.TrailingConfig.PartialEnabled = !session.User.Settings.TrailingConfig.PartialEnabled
	t.router.ApplySettings(ctx, session.User) // ✅ горячее применение
	t.handleSettingsMenu(ctx, chatID)
}
func (t *Telegram) toggleFeature(ctx context.Context, chatID int64, key string, cb *tgbot.CallbackQuery) {
	session, err := t.router.GetSession(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	if !session.User.Premium {
		return
	}
	ff := &session.User.Settings.FeatureFlags

	switch key {
	case "near_tp":
		ff.NearTPProtectEnabled = !ff.NearTPProtectEnabled
	case "simulate":
		ff.SimulateBeforeEntry = !ff.SimulateBeforeEntry
	case "chart":
		ff.DealChartEnabled = !ff.DealChartEnabled
	case "reco":
		ff.AutoRecommendEnabled = !ff.AutoRecommendEnabled
	case "pro":
		ff.ProMode = !ff.ProMode
	case "admin:tune:mode:toggle":
		t.router.ToggleTuneMode(ctx)

		// просто перерисуем меню настроек (оно возьмёт новый modeStr)
		t.handleSettingsMenuEdit(ctx, chatID, cb.Message.MessageID)
	default:
		_, _ = t.Send(ctx, chatID, "❗️Неизвестная фича")
		return
	}

	t.router.ApplySettings(ctx, session.User) // ✅ горячее применение
	t.handleSettingsMenu(ctx, chatID)

	_, _ = t.Send(ctx, chatID, "✅ Сохранено")
}
