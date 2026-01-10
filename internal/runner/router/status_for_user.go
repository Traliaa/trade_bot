package router

import (
	"context"
	"fmt"
	"trade_bot/internal/models"
	"trade_bot/internal/runner/sessions"
)

// StatusForUser возвращает позиции из кэша (без запроса в OKX).
func (r *Router) StatusForUser(ctx context.Context, userID int64) ([]models.OpenPosition, error) {
	r.mu.RLock()
	sess, ok := r.users[userID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("бот не запущен для этого пользователя")
	}

	return buildStatusFromCache(sess), nil
}

func buildStatusFromCache(sess *sessions.UserSession) []models.OpenPosition {
	// 1) snapshot cache
	sess.PosCacheMu.RLock()
	cacheAt := sess.PosCacheAt
	cache := make(map[models.PosKey]models.CachedPos, len(sess.PositionsCache))
	for k, v := range sess.PositionsCache {
		cache[k] = v
	}
	sess.PosCacheMu.RUnlock()

	// 2) snapshot trails
	sess.PosMu.RLock()
	trails := make(map[string]*models.PositionTrailState, len(sess.Positions))
	for k, v := range sess.Positions {
		trails[k] = v
	}
	sess.PosMu.RUnlock()

	out := make([]models.OpenPosition, 0, len(cache))

	for k, p := range cache {
		// базовое
		op := models.OpenPosition{
			Symbol:     p.InstID,
			Side:       p.PosSide, // "long"/"short"
			HoldVol:    p.Size,
			EntryPrice: p.Entry,
			LastPrice:  p.LastPx,
			Size:       p.Size,

			Updated: cacheAt,
			Status:  "OPEN",
		}

		// pnl (оценка по lastPx)
		if p.Entry > 0 && p.LastPx > 0 && p.Size > 0 {
			if p.PosSide == "long" {
				op.UnrealizedPnl = (p.LastPx - p.Entry) * p.Size
				op.UnrealizedPnlPct = (p.LastPx/p.Entry - 1) * 100
			} else {
				op.UnrealizedPnl = (p.Entry - p.LastPx) * p.Size
				op.UnrealizedPnlPct = (p.Entry/p.LastPx - 1) * 100 // грубая оценка
			}
		}

		// 3) overlay трейл-данных
		tk := p.InstID + ":" + p.PosSide
		if st := trails[tk]; st != nil {
			op.SL = st.SL
			op.TP = st.TP
			op.Entry = st.Entry
			op.Qty = st.Size
			// можно добавить в Status строку или отдельные поля если у тебя есть
			// op.Status = fmt.Sprintf("OPEN (BE=%v Lock=%v)", st.MovedToBE, st.LockedProfit)
		}

		out = append(out, op)

		_ = k // просто чтобы не ругался, если не используешь
	}

	return out
}
