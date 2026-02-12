package router

import (
	"context"
	"trade_bot/internal/runner/sessions"
)

func (r *Router) GetSession(ctx context.Context, userID int64) (*sessions.UserSession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.users[userID]
	if !ok {
		u, err := r.Repository.GetUser(ctx, userID, r.config)
		if err != nil {
			return nil, err
		}
		r.EnableUser(ctx, u)

		return r.GetSession(ctx, userID)
	}

	return s, nil
}
