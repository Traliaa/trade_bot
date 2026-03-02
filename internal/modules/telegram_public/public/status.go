// trade_bot/internal/telegram_public/public/status.go
package public

import (
	"fmt"
	"html"
	"strings"
	"time"
)

type State string

const (
	StateRestarting State = "restarting"
	StateConnecting State = "connecting"
	StatePreparing  State = "preparing"
	StateReady      State = "ready"
	StatePaused     State = "paused"
	StateError      State = "error"
)

type Status struct {
	State       State
	Exchange    string
	Instruments int
	Progress    int // 0..100
	UpdatedAt   time.Time

	PauseReason string
	ErrorHint   string
}

func (s Status) RenderHTML() string {
	var b strings.Builder

	// Экранируем только переменные данные (exchange/reason/hint).
	ex := esc(s.Exchange)
	reason := esc(s.PauseReason)
	hint := esc(s.ErrorHint)

	switch s.State {
	case StateRestarting:
		b.WriteString("⚠️ Бот перезапущен\n\n")
		b.WriteString("Восстанавливаем соединение и рыночные данные.\n")
		b.WriteString("⏳ Пожалуйста, подождите.\n")

	case StateConnecting:
		b.WriteString("📡 Подключаемся к бирже\n\n")
		if ex != "" {
			b.WriteString("Биржа: " + ex + "\n")
		}
		if s.Instruments > 0 {
			b.WriteString(fmt.Sprintf("Инструментов: %d\n", s.Instruments))
		}
		b.WriteString("\nГотовим поток рыночных данных...\n")

	case StatePreparing:
		b.WriteString("📊 Подготовка рыночных данных\n\n")
		if ex != "" {
			b.WriteString("Биржа: " + ex + "\n")
		}
		if s.Instruments > 0 {
			b.WriteString(fmt.Sprintf("Инструментов: %d\n", s.Instruments))
		}

		if s.Progress > 0 {
			p := clamp(s.Progress, 0, 99) // 100 оставим для StateReady
			b.WriteString("\n🔄 Готовность: ")
			b.WriteString(progressBar(p))
			b.WriteString("\n")
		} else {
			b.WriteString("\n⏳ Идёт подготовка...\n")
		}

	case StateReady:
		b.WriteString("🟢 Бот готов к работе\n\n")
		if ex != "" {
			b.WriteString("Биржа: " + ex + "\n")
		}
		if s.Instruments > 0 {
			b.WriteString(fmt.Sprintf("Инструментов: %d\n\n", s.Instruments))
		}
		b.WriteString("Система анализирует рынок в реальном времени\n")
		b.WriteString("и уведомит о появлении торговых сигналов.\n")

	case StatePaused:
		b.WriteString("🟡 Торговля приостановлена\n\n")
		b.WriteString("Бот продолжает наблюдать за рынком,\n")
		b.WriteString("но не будет открывать сделки.\n")
		if strings.TrimSpace(reason) != "" {
			b.WriteString("\nПричина: " + reason + "\n")
		}
		b.WriteString("\nЧтобы возобновить торговлю — откройте бота\n")
		b.WriteString("и нажмите «▶️ Запустить бота».\n")

	case StateError:
		b.WriteString("🔴 Ошибка запуска\n\n")
		b.WriteString("Не удалось подготовиться к работе.\n")
		b.WriteString("Бот попробует восстановиться автоматически.\n")
		if strings.TrimSpace(hint) != "" {
			b.WriteString("\nПодсказка: " + hint + "\n")
		}
		b.WriteString("\nЕсли вы пользователь — откройте бота и нажмите\n")
		b.WriteString("«▶️ Запустить бота».\n")
	}

	if !s.UpdatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("\n⏱ Обновлено: %s", s.UpdatedAt.Format("15:04:05")))
	}

	return b.String()
}

func esc(v string) string { return html.EscapeString(v) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// В HTML можно спокойно печатать "█" и "░" — Telegram их нормально рендерит.
func progressBar(percent int) string {
	total := 10
	filled := percent * total / 100
	if filled < 0 {
		filled = 0
	}
	if filled > total {
		filled = total
	}
	return fmt.Sprintf("[%s%s] %d%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", total-filled),
		percent,
	)
}
