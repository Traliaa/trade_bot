package service

import "context"

func (r *Service) DisableUser(ctx context.Context, userID int64) {
	r.mu.Lock()
	sess, ok := r.users[userID]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.users, userID)
	r.mu.Unlock()

	// ✅ останавливаем воркеры
	if sess.Cancel != nil {
		sess.Cancel()
	}

}
