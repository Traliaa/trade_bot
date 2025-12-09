package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"trade_bot/internal/exchange"
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

func (m *Manager) StatusForUser(ctx context.Context, user *models.UserSettings) (string, error) {
	// тут либо переиспользуешь существующий exchange.Client,
	// либо создаёшь временный
	mx := exchange.NewClient(user) // подставь свой конструктор

	positions, err := mx.OpenPositions(ctx)
	if err != nil {
		return "", fmt.Errorf("open positions: %w", err)
	}

	if len(positions) == 0 {
		return "📊 Открытых позиций нет.", nil
	}

	var b strings.Builder
	b.WriteString("*Открытые позиции:*\n\n")

	var totalPnl float64

	for _, p := range positions {
		// подгони поля под свой тип PositionInfo
		symbol := p.Symbol
		side := strings.ToUpper(p.Side) // BUY/SELL или long/short
		qty := p.Size                   // размер
		entry := p.EntryPrice           // средняя цена входа
		last := p.LastPrice             // последняя цена
		upnl := p.UnrealizedPnl         // PnL в USDT
		upnlPct := p.UnrealizedPnlPct   // PnL в %

		totalPnl += upnl

		fmt.Fprintf(&b,
			"[%s] %s\n"+
				"  Размер: `%.4f`\n"+
				"  Вход:   `%.4f`\n"+
				"  Сейчас: `%.4f`\n"+
				"  PnL:    `%.2f USDT (%.2f%%)`\n\n",
			symbol, side,
			qty,
			entry,
			last,
			upnl, upnlPct,
		)
	}

	fmt.Fprintf(&b, "*Суммарный PnL:* `%.2f USDT`\n", totalPnl)

	return b.String(), nil
}
