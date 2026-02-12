package router

import "trade_bot/internal/models"

func (r *Router) ApplySettings(user *models.UserSettings) {
	if user == nil {
		return
	}

	r.mu.RLock()
	sess := r.users[user.UserID]
	r.mu.RUnlock()

	if sess == nil {
		return
	}

	// обновляем параметры стратегии/риска/трейлинга/фичей
	sess.UpdateSettings(user.Settings)

	// если обновили ключи/пасфразу — обновим клиента
	// (можно всегда обновлять — это дешево, но лучше по условию)
	ts := user.Settings.TradingSettings
	if ts.OKXAPIKey != "" && ts.OKXAPISecret != "" && ts.OKXPassphrase != "" {
		sess.UpdateOKXClient(user)
	}
}
