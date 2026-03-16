package service

import (
	"context"
	"fmt"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/runner_old/sessions"
)

// StatusForUser возвращает список открытых позиций с OKX для данного пользователя.
func (r *Service) StatusForUser(ctx context.Context, userID int64) ([]models.OpenPosition, error) {
	r.mu.RLock()
	sess := r.users[userID]
	r.mu.RUnlock()

	//TODO убрать после рефаткоринга

	user, _ := r.GetUser(ctx, userID)
	sess, _ = r.EnableUser(ctx, user)

	if sess == nil {
		return nil, fmt.Errorf("бот не запущен для этого пользователя")
	}

	// ✅ ИСТОЧНИК ПРАВДЫ — OKX positions
	pos, err := sess.Okx.OpenPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("okx positions: %w", err)
	}
	return pos, nil
}

func buildStatusFromCache(sess *sessions.UserSession) []models.OpenPosition {
	// 1) snapshot cache
	sess.ExchangeMu.RLock()
	cacheAt := sess.ExchangePositionsAt
	cache := make(map[models.PosKey]models.CachedPos, len(sess.ExchangePositions))
	for k, v := range sess.ExchangePositions {
		cache[k] = v
	}
	sess.ExchangeMu.RUnlock()

	// 2) snapshot trails
	sess.TrailMu.RLock()
	trails := make(map[models.PosKey]*models.PositionTrailState, len(sess.TrailStates))
	for k, v := range sess.TrailStates {
		trails[k] = v
	}
	sess.TrailMu.RUnlock()

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
		tk := models.PosKey{p.InstID, p.PosSide}
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
