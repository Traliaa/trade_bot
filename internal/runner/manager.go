package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/okx_client/service"
	okx_websocket "trade_bot/internal/modules/okx_websocket/service"
)

type SettingsStore interface {
	Get(userID int64) (*models.UserSettings, bool)
	Save(*models.UserSettings) error
}

// Manager управляет раннерами для разных юзеров.
type Manager struct {
	mu      sync.Mutex
	runners map[int64]*Runner
	mkt     *okx_websocket.Client
}

func NewManager(mkt *okx_websocket.Client) *Manager {
	return &Manager{
		mkt:     mkt,
		runners: make(map[int64]*Runner),
	}
}

// StopForUser останавливает воркер для конкретного юзера (если запущен).
func (m *Manager) StopForUser(ctx context.Context, user *models.UserSettings) error {
	m.mu.Lock()
	r, ok := m.runners[user.TelegramID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("runner not running for user %d", user.TelegramID)
	}
	// Можно заранее удалить, чтобы второй вызов не прошёл
	delete(m.runners, user.TelegramID)
	m.mu.Unlock()

	// Гасим раннер вне мьютекса
	r.Stop()

	return nil
}

func (m *Manager) StatusForUser(ctx context.Context, user *models.UserSettings) (string, error) {
	// тут либо переиспользуешь существующий exchange.Client,
	// либо создаёшь временный
	mx := service.NewClient(user) // подставь свой конструктор

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
