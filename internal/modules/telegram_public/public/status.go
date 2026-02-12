package public

import (
	"fmt"
	"strings"
	"time"
)

type State string

const (
	StateRestarting State = "restarting"
	StatePreparing  State = "preparing"
	StateReady      State = "ready"
	StatePaused     State = "paused"
	StateError      State = "error"
)

type Status struct {
	State       State
	Exchange    string
	Instruments int
	Progress    int
	UpdatedAt   time.Time

	PauseReason string
	ErrorHint   string
}

func (s Status) Render() string {
	var b strings.Builder

	switch s.State {
	case StateRestarting:
		b.WriteString("⚠️ *Бот перезапущен*\n\n")
		b.WriteString("Восстанавливаем соединение и рыночные данные.\n")
		b.WriteString("⏳ Пожалуйста, подождите.\n")

	case StatePreparing:
		b.WriteString("📊 *Подготовка рыночных данных*\n\n")
		if s.Exchange != "" {
			b.WriteString(fmt.Sprintf("Биржа: %s\n", s.Exchange))
		}
		if s.Instruments > 0 {
			b.WriteString(fmt.Sprintf("Инструментов: %d\n", s.Instruments))
		}
		if s.Progress > 0 {
			if s.Progress > 99 {
				s.Progress = 99
			}
			b.WriteString("\n🔄 Готовность: ")
			b.WriteString(progressBar(s.Progress))
			b.WriteString("\n")
		} else {
			b.WriteString("\n⏳ Идёт подготовка...\n")
		}

	case StateReady:
		b.WriteString("🟢 *Бот готов к работе*\n\n")
		if s.Exchange != "" {
			b.WriteString(fmt.Sprintf("Биржа: %s\n", s.Exchange))
		}
		if s.Instruments > 0 {
			b.WriteString(fmt.Sprintf("Инструментов: %d\n\n", s.Instruments))
		}
		b.WriteString("Теперь бот анализирует рынок и пришлёт сигнал,\n")
		b.WriteString("когда появятся подходящие условия.\n")

	case StatePaused:
		b.WriteString("🟡 *Торговля приостановлена*\n\n")
		b.WriteString("Бот продолжает наблюдать за рынком,\n")
		b.WriteString("но *не будет открывать сделки*.\n")
		if strings.TrimSpace(s.PauseReason) != "" {
			b.WriteString("\nПричина: ")
			b.WriteString(s.PauseReason)
			b.WriteString("\n")
		}
		b.WriteString("\nЧтобы возобновить торговлю — откройте бота\n")
		b.WriteString("и нажмите *▶️ Запустить бота*.\n")

	case StateError:
		b.WriteString("🔴 *Ошибка запуска*\n\n")
		b.WriteString("Не удалось подготовиться к работе.\n")
		b.WriteString("Бот попробует восстановиться автоматически.\n")
		if strings.TrimSpace(s.ErrorHint) != "" {
			b.WriteString("\nПодсказка: ")
			b.WriteString(s.ErrorHint)
			b.WriteString("\n")
		}
	}

	if !s.UpdatedAt.IsZero() {
		b.WriteString(fmt.Sprintf("\n⏱ Обновлено: %s", s.UpdatedAt.Format("15:04:05")))
	}

	return b.String()
}

func progressBar(percent int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	total := 10
	filled := percent * total / 100
	return fmt.Sprintf("[%s%s] %d%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", total-filled),
		percent,
	)
}
