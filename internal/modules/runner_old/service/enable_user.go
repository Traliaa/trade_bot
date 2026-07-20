package service

import (
	"context"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/runner_old/sessions"

	"go.uber.org/zap"
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
		Base:              r.Base,
		Notifier:          r.TelegramNotifier,
		ExchangePositions: make(map[models.PosKey]models.CachedPos),
		TrailStates:       make(map[models.PosKey]*models.PositionTrailState),
		User:              &u,
		Ctx:               runCtx,
		Cancel:            cancel,
		Repo:              r.Repository,
		LastMsgAt:         make(map[string]time.Time),
		Config:            r.config,
		V3PartialCheck: func(instID string) bool {
			return r.strategy.CheckV3Partial(instID)
		},
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
	if err := sess.RefreshAccountSnapshot(ctx); err != nil {
		r.Logger.Warn("initial account snapshot refresh failed",
			zap.Error(err),
			zap.Int64("userID", user.TelegramID),
		)
	}
	if err := sess.RestoreTrailStates(ctx); err != nil {
		r.Logger.Warn("restore trail states failed",
			zap.Error(err),
			zap.Int64("userID", user.TelegramID),
		)
	}
	if err := sess.SyncClosedTrades(ctx); err != nil {
		r.Logger.Warn("sync closed trades after restore failed",
			zap.Error(err),
			zap.Int64("userID", user.TelegramID),
		)
	}
	go sess.TradeHistoryWorker(runCtx)

	go sess.StartAccountRefresher(ctx, 10*time.Minute)

	sess.User.Status = true

	r.ApplySettings(ctx, sess.User)
	return sess, true
}
