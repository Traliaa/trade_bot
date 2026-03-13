package service

import (
	"context"
	"trade_bot/internal/models"
	"trade_bot/pkg/logger"
)

func (r *Service) ApplySettings(ctx context.Context, user *models.UserSettings) {
	if user == nil {
		return
	}

	r.mu.RLock()
	sess := r.users[user.TelegramID]
	r.mu.RUnlock()

	if sess == nil {
		return
	}

	// обновляем параметры стратегии/риска/трейлинга/фичей
	sess.UpdateSettings(user.Settings)

	if err := r.Repository.Update(ctx, user); err != nil {
		logger.Error("⚠️ Не удалось применить пресет")
		return
	}

	// если обновили ключи/пасфразу — обновим клиента
	// (можно всегда обновлять — это дешево, но лучше по условию)
	ts := user.Settings.TradingSettings
	if ts.OKXAPIKey != "" && ts.OKXAPISecret != "" && ts.OKXPassphrase != "" {
		sess.UpdateOKXClient(user)
	}
}
