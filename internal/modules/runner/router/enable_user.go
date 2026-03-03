package router

import (
	"context"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/runner/sessions"
)

func (r *Router) EnableUser(ctx context.Context, user *models.UserSettings) {
	if user == nil {
		return
	}

	r.mu.Lock()
	if _, ok := r.users[user.TelegramID]; ok {
		r.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	sess := &sessions.UserSession{
		Notifier:       r.TelegramNotifier,
		PositionsCache: make(map[models.PosKey]models.CachedPos),
		Positions:      make(map[string]*models.PositionTrailState),
		User:           user,
		Ctx:            ctx,
		Cancel:         cancel,
		Repo:           r.Repository,
		LastMsgAt:      make(map[string]time.Time),
	}

	// ✅ инициализируем настройки (снэпшот)
	sess.InitSettings(user.Settings)

	// ✅ инициализируем OKX клиента из user (ключи)
	sess.UpdateOKXClient(user)

	r.users[user.TelegramID] = sess
	r.mu.Unlock()

	go sess.PositionCacheWorker(ctx)

	if sess.User.Premium {
		go sess.PositionGuardWorker(ctx)
	}

}
