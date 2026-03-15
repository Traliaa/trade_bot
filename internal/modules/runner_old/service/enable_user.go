package service

import (
	"context"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/runner_old/sessions"
)

func (r *Service) EnableUser(ctx context.Context, user *models.UserSettings) (*sessions.UserSession, bool) {
	if user == nil {
		return nil, false
	}

	r.mu.Lock()
	if existing, ok := r.users[user.TelegramID]; ok {
		r.mu.Unlock()
		return existing, false
	}

	runCtx, cancel := context.WithCancel(context.Background())

	// Важно: сохраняем копию, а не внешний указатель.
	u := *user

	sess := &sessions.UserSession{
		Base:           r.Base,
		Notifier:       r.TelegramNotifier,
		PositionsCache: make(map[models.PosKey]models.CachedPos),
		Positions:      make(map[string]*models.PositionTrailState),
		User:           &u,
		Ctx:            runCtx,
		Cancel:         cancel,
		Repo:           r.Repository,
		LastMsgAt:      make(map[string]time.Time),
	}

	//sess.USDTBalance(ctx)
	// Снапшот настроек.
	sess.InitSettings(u.Settings)

	// Инициализация OKX клиента.
	sess.UpdateOKXClient(&u)

	r.users[u.TelegramID] = sess
	r.mu.Unlock()

	go sess.PositionCacheWorker(runCtx)

	if sess.User.Premium {
		go sess.PositionGuardWorker(runCtx)
	}

	go sess.TradeHistoryWorker(runCtx)

	return sess, true
}
