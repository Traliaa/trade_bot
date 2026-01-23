package service

import "context"

func (t *Telegram) toggleConfirm(ctx context.Context, chatID int64) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}
	ts := &user.Settings.TradingSettings
	ts.ConfirmRequired = !ts.ConfirmRequired
	_ = t.repo.Update(ctx, user)
	t.handleSettingsMenu(ctx, chatID)
}

func (t *Telegram) togglePartial(ctx context.Context, chatID int64) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	user.Settings.TrailingConfig.PartialEnabled = !user.Settings.TrailingConfig.PartialEnabled

	if err := t.repo.Update(ctx, user); err != nil {
		_, _ = t.Send(ctx, chatID, "⚠️ Не удалось сохранить: "+err.Error())
		return
	}

	t.handleTrailingMenu(ctx, chatID)
}
func (t *Telegram) toggleFeature(ctx context.Context, chatID int64, key string) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	ff := &user.Settings.FeatureFlags

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
	default:
		_, _ = t.Send(ctx, chatID, "❗️Неизвестная фича")
		return
	}

	if err := t.repo.Update(ctx, user); err != nil {
		_, _ = t.Send(ctx, chatID, "⚠️ Не удалось сохранить: "+err.Error())
		return
	}

	_, _ = t.Send(ctx, chatID, "✅ Сохранено")
	t.handleFeaturesMenu(ctx, chatID)
}
