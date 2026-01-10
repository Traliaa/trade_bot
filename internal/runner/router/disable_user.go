package router

func (r *Router) DisableUser(userID int64) {
	r.mu.Lock()
	sess, ok := r.users[userID]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.users, userID)

	// вырезаем из индекса
	for k, list := range r.index {
		n := list[:0]
		for _, s := range list {
			if s.UserID != userID {
				n = append(n, s)
			}
		}
		if len(n) == 0 {
			delete(r.index, k)
		} else {
			r.index[k] = n
		}
	}
	r.mu.Unlock()

	// ✅ останавливаем воркеры
	if sess.Cancel != nil {
		sess.Cancel()
	}

	// ✅ останавливаем confirmWorker
	close(sess.Queue)
}
