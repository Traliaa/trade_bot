// sessions/position_guard.go
package sessions

import (
	"context"
	"strings"
	"time"

	"trade_bot/internal/models"
)

func posKey(instID, side string) string {
	return instID + ":" + strings.ToLower(strings.TrimSpace(side))
}

func (s *UserSession) PositionGuardWorker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// при старте тоже проверим (чтобы быстро поймать ручные позиции)
	s.guardOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.guardOnce(ctx)
		}
	}
}

func (s *UserSession) guardOnce(ctx context.Context) {
	// 1) берём реальные позиции с OKX
	positions, err := s.Okx.OpenPositions(ctx)
	if err != nil {
		// не спамим в личку постоянно, но можно раз в N часов
		s.Notifier.SendF(ctx, s.User.TelegramID, "⚠️ Не удалось проверить позиции на OKX: %v", err)
		return
	}

	// 2) инициализация мапы в настройках
	if s.User.Settings.PositionGuard == nil {
		s.User.Settings.PositionGuard = make(models.PositionGuardMap)
	}

	now := time.Now()

	for _, p := range positions {
		key := posKey(p.Symbol, p.Side)

		st := s.User.Settings.PositionGuard[key]
		// Legacy warning exhaustion must not disable safety checks forever.
		st.Blacklisted = false

		// анти-спам: чаще чем раз в час не пишем
		if !st.LastWarnAt.IsZero() && now.Sub(st.LastWarnAt) < 1*time.Hour {
			continue
		}

		// 3) проверяем TP/SL
		hasTP, hasSL, err := s.Okx.HasTpSl(ctx, p.Symbol, p.Side)
		if err != nil {
			st.LastWarnAt = now
			s.User.Settings.PositionGuard[key] = st
			_ = s.saveSettings(ctx)
			s.Notifier.SendF(ctx, s.User.TelegramID,
				"⚠️ [%s %s] Не удалось проверить защитные ордера TP/SL: %v. Наличие защиты не подтверждено.", p.Symbol, strings.ToUpper(p.Side), err)
			continue
		}

		if hasTP && hasSL {
			// если всё ок — сбрасывать warnCount не обязательно, но можно
			continue
		}

		// 4) нет TP/SL → предупреждаем и учитываем
		st.WarnCount++
		st.LastWarnAt = now

		missing := []string{}
		if !hasSL {
			missing = append(missing, "SL")
		}
		if !hasTP {
			missing = append(missing, "TP")
		}

		s.User.Settings.PositionGuard[key] = st
		_ = s.saveSettings(ctx) // см. ниже

		s.Notifier.SendF(ctx, s.User.TelegramID,
			"⚠️ [%s %s] Не найдены активные защитные ордера: %s. Проверь позицию на OKX. Автоматически восстанавливать старые уровни небезопасно; сопровождение ботом не отключено.",
			p.Symbol, strings.ToUpper(p.Side), strings.Join(missing, "+"))
	}
}

// saveSettings — единая точка сохранения user settings в repo
func (s *UserSession) saveSettings(ctx context.Context) error {
	if s.Repo == nil {
		return nil
	}
	return s.Repo.Update(ctx, s.User)

}
