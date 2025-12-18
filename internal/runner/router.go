// internal/runner/router.go
package runner

import (
	"context"
	"fmt"
	"sync"
	"time"
	okx_client "trade_bot/internal/modules/okx_client/service"

	"trade_bot/internal/models"
)

type UserSettingsSnapshot struct {
	UserID   int64
	Settings *models.UserSettings
	Runner   *Runner
}

// Router хранит активных юзеров и раздаёт сигналы.
type Router struct {
	mu    sync.RWMutex
	users map[int64]*userSession    // userID -> сессия
	index map[string][]*userSession // key(tf,strategy) -> сессии
}

func NewRouter() *Router {
	return &Router{
		users: make(map[int64]*userSession),
		index: make(map[string][]*userSession),
	}
}

// key по таймфрейму и стратегии
func key(tf string, st models.StrategyType) string {
	return tf + "::" + string(st)
}

func (r *Router) EnableUser(user *models.UserSettings, n TelegramNotifier) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[user.UserID]; ok {
		// уже включен
		return
	}

	sess := &userSession{
		userID:      user.UserID,
		settings:    user,
		notifier:    n,
		okx:         okx_client.NewClient(user),
		queue:       make(chan models.Signal, 64),
		pending:     make(map[string]bool),
		cooldownTil: make(map[string]time.Time),
	}

	r.users[user.UserID] = sess

	k := key(user.TradingSettings.Timeframe, user.TradingSettings.Strategy)
	r.index[k] = append(r.index[k], sess)

	// стартуем воркер подтверждений для юзера
	go sess.confirmWorker(context.Background())
}

func (r *Router) DisableUser(userID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess, ok := r.users[userID]
	if !ok {
		return
	}
	delete(r.users, userID)

	// вырезаем из индекса
	for k, list := range r.index {
		n := list[:0]
		for _, s := range list {
			if s.userID != userID {
				n = append(n, s)
			}
		}
		if len(n) == 0 {
			delete(r.index, k)
		} else {
			r.index[k] = n
		}
	}

	close(sess.queue) // этим аккуратно гасим confirmWorker
}

func (r *Router) OnSignal(ctx context.Context, sig models.Signal) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	k := key(sig.TF, sig.Strategy)
	sessions := r.index[k]
	if len(sessions) == 0 {
		return
	}

	for _, sess := range sessions {
		select {
		case sess.queue <- sig:
		default:
			// очередь забита — можно логнуть / дропнуть
		}
	}
}

// StatusForUser возвращает открытые позиции для конкретного юзера.
// Используется кнопкой "📊 Статус" в Telegram.
func (r *Router) StatusForUser(ctx context.Context, userID int64) ([]models.OpenPosition, error) {
	r.mu.RLock()
	sess, ok := r.users[userID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("бот не запущен для этого пользователя")
	}

	return sess.Status(ctx)
}
