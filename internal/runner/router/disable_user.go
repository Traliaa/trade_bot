package router

func (r *Router) DisableUser(userID int64) {
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

	// ✅ останавливаем confirmWorker
	close(sess.Queue)
}
