package router

import (
	"context"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	okx_client "trade_bot/internal/modules/okx_client/service"

	"trade_bot/internal/runner/sessions"
)

func (r *Router) EnableUser(user *models.UserSettings, n TelegramNotifier) {
	// 1) быстро проверяем/создаём sess под локом
	r.mu.Lock()
	if _, ok := r.users[user.UserID]; ok {
		r.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())

	sess := &sessions.UserSession{
		UserID:   user.UserID,
		Settings: user,
		Notifier: n,
		Okx:      okx_client.NewClient(user),

		Queue:       make(chan models.Signal, 64),
		Pending:     make(map[string]bool),
		CooldownTil: make(map[string]time.Time),

		PositionsCache: make(map[models.PosKey]models.CachedPos),
		Positions:      make(map[string]*models.PositionTrailState),

		Ctx:    ctx,
		Cancel: cancel,
	}

	r.users[user.UserID] = sess

	k := helper.Key(user.TradingSettings.Timeframe, user.TradingSettings.Strategy)
	r.index[k] = append(r.index[k], sess)

	r.mu.Unlock()

	// 2) воркеры запускаем уже без лока роутера
	go sess.ConfirmWorker(ctx)
	go sess.PositionCacheWorker(ctx)
}
