package service

import (
	"context"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/runner_old/sessions"
)

func (r *Service) GetSession(userID int64) (*sessions.UserSession, bool) {
	r.mu.RLock()
	s, ok := r.users[userID]
	r.mu.RUnlock()

	return s, ok
}
func (r *Service) GetUser(ctx context.Context, userID int64) (*models.UserSettings, error) {
	return r.Repository.GetUser(ctx, userID, r.config)
}
