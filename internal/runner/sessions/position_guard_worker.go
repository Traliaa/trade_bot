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
		s.Notifier.SendF(ctx, s.UserID, "⚠️ Не удалось проверить позиции на OKX: %v", err)
		return
	}

	// 2) инициализация мапы в настройках
	if s.Settings.Settings.PositionGuard == nil {
		s.Settings.Settings.PositionGuard = make(models.PositionGuardMap)
	}

	now := time.Now()

	for _, p := range positions {
		key := posKey(p.Symbol, p.Side)

		st := s.Settings.Settings.PositionGuard[key]
		if st.Blacklisted {
			continue
		}

		// анти-спам: чаще чем раз в час не пишем
		if !st.LastWarnAt.IsZero() && now.Sub(st.LastWarnAt) < 1*time.Hour {
			continue
		}

		// 3) проверяем TP/SL
		hasTP, hasSL, err := s.Okx.HasTpSl(ctx, p.Symbol, p.Side)
		if err != nil {
			// если не можем проверить — лучше не помечать, просто пропускаем (или пишем редко)
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

		// после 5 предупреждений — в блэклист
		if st.WarnCount >= 5 {
			st.Blacklisted = true
		}

		s.Settings.Settings.PositionGuard[key] = st
		_ = s.saveSettings(ctx) // см. ниже

		if st.Blacklisted {
			s.Notifier.SendF(ctx, s.UserID,
				"⛔️ [%s %s] Нет %s. Я предупреждал уже 5 раз.\n"+
					"Эту позицию больше не трогаю и не напоминаю.\n"+
					"Если хочешь — закрой её или выставь SL/TP вручную.",
				p.Symbol, strings.ToUpper(p.Side), strings.Join(missing, "+"),
			)
		} else {
			s.Notifier.SendF(ctx, s.UserID,
				"⚠️ [%s %s] В позиции НЕ выставлены: *%s*\n"+
					"Я не буду сопровождать эту позицию (BE/Lock/Partial/TimeStop не применяются),\n"+
					"пока ты не поставишь SL и TP на OKX.\n\n"+
					"Напоминание %d/5. (Потом замолчу)",
				p.Symbol, strings.ToUpper(p.Side), strings.Join(missing, "+"),
				st.WarnCount,
			)
		}
	}
}

// saveSettings — единая точка сохранения user settings в repo
func (s *UserSession) saveSettings(ctx context.Context) error {
	if s.Repo == nil {
		return nil
	}
	return s.Repo.Update(ctx, s.Settings)

}
