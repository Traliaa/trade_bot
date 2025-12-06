package runner

import (
	"context"
	"fmt"
	"sync"
	"trade_bot/internal/models"
)

type SettingsStore interface {
	Get(userID int64) (*models.UserSettings, bool)
	Save(*models.UserSettings) error
}

// Manager управляет раннерами для разных юзеров.
type Manager struct {
	mu      sync.Mutex
	runners map[int64]*Runner
}

func NewManager() *Manager {
	return &Manager{

		runners: make(map[int64]*Runner),
	}
}

// RunForUser стартует воркер для конкретного юзера (если ещё не запущен).
func (m *Manager) RunForUser(ctx context.Context, user *models.UserSettings, t TelegramNotifier) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, running := m.runners[user.UserID]; running {
		// уже запущен — можно вернуть nil или ошибку, как удобнее
		return fmt.Errorf("runner already running for user %d", user.UserID)
	}

	// 4. Runner для юзера
	r := New(user, t)
	m.runners[user.UserID] = r

	// 5. Запускаем в отдельной горутине
	go func() {
		// по-хорошему, сюда лучше передавать дочерний ctx с отменой
		r.Start(ctx)

		// когда Start закончится — выпилим раннер из мапы
		m.mu.Lock()
		delete(m.runners, user.UserID)
		m.mu.Unlock()
	}()

	return nil
}

// StopForUser останавливает воркер для конкретного юзера (если запущен).
func (m *Manager) StopForUser(ctx context.Context, user *models.UserSettings) error {
	m.mu.Lock()
	r, ok := m.runners[user.UserID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("runner not running for user %d", user.UserID)
	}
	// Можно заранее удалить, чтобы второй вызов не прошёл
	delete(m.runners, user.UserID)
	m.mu.Unlock()

	// Гасим раннер вне мьютекса
	r.Stop()

	return nil
}
