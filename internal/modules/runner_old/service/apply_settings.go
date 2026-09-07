package service

import (
	"context"
	"errors"
	"trade_bot/internal/models"
)

func (r *Service) ApplySettings(ctx context.Context, user *models.UserSettings) error {
	if user == nil {
		return errors.New("user is required")
	}

	// Persist even when the bot is stopped. Only publish successfully saved settings.
	if err := r.Repository.Update(ctx, user); err != nil {
		return err
	}

	r.mu.RLock()
	sess := r.users[user.TelegramID]
	r.mu.RUnlock()

	if sess == nil {
		return nil
	}

	// обновляем параметры стратегии/риска/трейлинга/фичей
	sess.UpdateSettings(user.Settings)

	// если обновили ключи/пасфразу — обновим клиента
	// (можно всегда обновлять — это дешево, но лучше по условию)
	ts := user.Settings.TradingSettings
	if ts.OKXAPIKey != "" && ts.OKXAPISecret != "" && ts.OKXPassphrase != "" {
		sess.UpdateOKXClient(user)
	}
	return nil
}
