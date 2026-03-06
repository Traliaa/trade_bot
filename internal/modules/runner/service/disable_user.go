package service

import "context"

func (r *Service) DisableUser(ctx context.Context, userID int64) bool {
	r.mu.Lock()
	sess, ok := r.users[userID]
	if !ok {
		r.mu.Unlock()
		return false
	}
	delete(r.users, userID)
	r.mu.Unlock()

	if sess.Cancel != nil {
		sess.Cancel()
	}

	return true
}
