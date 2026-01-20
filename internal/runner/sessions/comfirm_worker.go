package sessions

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/models"
)

func (s *UserSession) ConfirmWorker(ctx context.Context) {

	for sig := range s.Queue {
		fmt.Printf("[CONF WORKER] user=%d got sig %s %s tf=%s\n", s.UserID, sig.InstID, sig.Side, sig.TF)

		// 0) кулдаун и pending по символу
		if s.isCooldown(sig.InstID) || s.isPending(sig.InstID) {
			continue
		}
		s.setPending(sig.InstID, true)

		// гарантированно снимаем pending при любом выходе
		func() {
			defer s.setPending(sig.InstID, false)

			// 1) лимит по открытым позициям
			if s.Settings.TradingSettings.MaxOpenPositions > 0 {
				if positions, err := s.Okx.OpenPositions(ctx); err == nil &&
					len(positions) >= s.Settings.TradingSettings.MaxOpenPositions {
					if s.canSend("limit_open_positions", 30*time.Minute) {
						s.Notifier.SendF(ctx, s.UserID,
							"⚠️ [%s] Лимит открытых позиций (%d) достигнут, сигнал пропущен",
							sig.InstID, s.Settings.TradingSettings.MaxOpenPositions,
						)
					}
					return
				}
			}

			// 2) Confirm (если включен)
			prompt := fmt.Sprintf(
				"🔔 [%s] %s %s @ %.4f\n%s\nSL/TP будут выставлены после входа. Войти?",
				sig.InstID, sig.Strategy, sig.Side, sig.Price, sig.Reason,
			)

			ok := true
			if s.Settings.TradingSettings.ConfirmRequired {
				ok = s.Notifier.Confirm(ctx, s.UserID, prompt, s.Settings.TradingSettings.ConfirmTimeout)
			}
			if !ok {
				s.setCooldown(sig.InstID, time.Now().Add(s.Settings.TradingSettings.CooldownPerSymbol))
				s.Notifier.SendF(ctx, s.UserID, "⛔️ [%s] Вход отменён/таймаут", sig.InstID)
				return
			}

			// 3) расчёт параметров
			params, err := s.calcTradeParams(ctx, sig.InstID, string(sig.Side), sig.Price)
			if err != nil {
				s.Notifier.SendF(ctx, s.UserID,
					"❗️ [%s] Ошибка расчёта параметров сделки: %v", sig.InstID, err)
				return
			}

			// 4) открытие + TP/SL
			res, err := s.openPositionWithTpSl(ctx, sig, params)
			if err != nil {
				s.Notifier.SendF(ctx, s.UserID,
					"❗️ [%s] Ошибка открытия ордера: %v", sig.InstID, err)
				return
			}

			// 5) сохраняем трейл-состояние (только если есть SL algoId)
			if res.SLAlgoID == "" {
				// трейлинг не сможет двигать SL — просто выходим
				return
			}

			key := sig.InstID + ":" + res.PosSide

			s.PosMu.Lock()
			if s.Positions == nil {
				s.Positions = make(map[string]*models.PositionTrailState)
			}
			s.Positions[key] = &models.PositionTrailState{
				InstID:   sig.InstID,
				PosSide:  res.PosSide,
				Entry:    res.Entry,
				SL:       params.SL,
				TP:       params.TP,
				RiskDist: params.RiskDist,
				TickSz:   params.TickSize,
				AlgoID:   res.SLAlgoID, // ✅ SL algoId
				Size:     params.Size,
				MFE:      res.Entry,
				OpenedAt: time.Now(),
			}
			s.PosMu.Unlock()
		}()
	}
}

// ----- helpers под s.mu -----

func (s *UserSession) isCooldown(instID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if until, ok := s.CooldownTil[instID]; ok && time.Now().Before(until) {
		return true
	}
	return false
}

func (s *UserSession) isPending(instID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Pending[instID]
}

func (s *UserSession) setPending(instID string, v bool) {
	s.mu.Lock()
	if s.Pending == nil {
		s.Pending = make(map[string]bool)
	}
	s.Pending[instID] = v
	s.mu.Unlock()
}

func (s *UserSession) setCooldown(instID string, until time.Time) {
	s.mu.Lock()
	if s.CooldownTil == nil {
		s.CooldownTil = make(map[string]time.Time)
	}
	s.CooldownTil[instID] = until
	s.mu.Unlock()
}
