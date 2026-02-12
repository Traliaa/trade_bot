package router

import (
	"context"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/runner/sessions"
)

func (r *Router) EnableUser(user *models.UserSettings) {
	if user == nil {
		return
	}

	r.mu.Lock()
	if _, ok := r.users[user.UserID]; ok {
		r.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	sess := &sessions.UserSession{
		UserID:         user.UserID,
		Notifier:       r.TelegramNotifier,
		PositionsCache: make(map[models.PosKey]models.CachedPos),
		Positions:      make(map[string]*models.PositionTrailState),
		Settings:       user,
		Ctx:            ctx,
		Cancel:         cancel,
		Repo:           r.Repository,
		LastMsgAt:      make(map[string]time.Time),
	}

	// ✅ инициализируем настройки (снэпшот)
	sess.InitSettings(user.Settings)

	// ✅ инициализируем OKX клиента из user (ключи)
	sess.UpdateOKXClient(user)

	r.users[user.UserID] = sess
	r.mu.Unlock()

	go sess.PositionCacheWorker(ctx)

	if sess.Settings.Premium {
		go sess.PositionGuardWorker(ctx)
	}

}
