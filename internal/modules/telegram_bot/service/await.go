package service

import "sync"

type awaitStore struct {
	mu sync.Mutex
	m  map[int64]string // chatID -> key
}

func newAwaitStore() *awaitStore {
	return &awaitStore{m: make(map[int64]string)}
}

func (t *Telegram) setAwait(chatID int64, key string) {
	t.await.mu.Lock()
	defer t.await.mu.Unlock()
	t.await.m[chatID] = key
}

func (t *Telegram) popAwait(chatID int64) string {
	t.await.mu.Lock()
	defer t.await.mu.Unlock()
	key := t.await.m[chatID]
	delete(t.await.m, chatID)
	return key
}
func (t *Telegram) peekAwait(chatID int64) (string, bool) {
	t.await.mu.Lock()
	defer t.await.mu.Unlock()
	key, ok := t.await.m[chatID]
	return key, ok
}

func (t *Telegram) clearAwait(chatID int64) {
	t.await.mu.Lock()
	defer t.await.mu.Unlock()
	delete(t.await.m, chatID)
}
