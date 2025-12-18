package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/models"
	okx_client "trade_bot/internal/modules/okx_client/service"
)

type userSession struct {
	userID   int64
	settings *models.UserSettings
	notifier TelegramNotifier
	okx      *okx_client.Client

	queue       chan models.Signal
	pending     map[string]bool
	cooldownTil map[string]time.Time

	mu sync.Mutex
}

func (s *userSession) confirmWorker(ctx context.Context) {
	for sig := range s.queue {
		// 0. кулдаун и pending по символу
		s.mu.Lock()
		if until, ok := s.cooldownTil[sig.InstID]; ok && time.Now().Before(until) {
			s.mu.Unlock()
			continue
		}
		if s.pending[sig.InstID] {
			s.mu.Unlock()
			continue
		}
		s.pending[sig.InstID] = true
		s.mu.Unlock()

		// 1. лимит по открытым позициям
		if s.settings.TradingSettings.MaxOpenPositions > 0 {
			if positions, err := s.okx.OpenPositions(ctx); err == nil &&
				len(positions) >= s.settings.TradingSettings.MaxOpenPositions {
				s.setPending(sig.InstID, false)
				s.notifier.SendF(ctx, s.userID,
					"⚠️ [%s] Лимит открытых позиций (%d) достигнут, сигнал пропущен",
					sig.InstID, s.settings.TradingSettings.MaxOpenPositions,
				)
				continue
			}
		}

		// 2. Confirm (если включен)
		prompt := fmt.Sprintf(
			"🔔 [%s] %s %s @ %.4f\n%s\nSL/TP будут выставлены после входа. Войти?",
			sig.InstID, sig.Strategy, sig.Side, sig.Price, sig.Reason,
		)

		ok := true
		if s.settings.TradingSettings.ConfirmRequired {
			ok = s.notifier.Confirm(ctx, s.userID, prompt, s.settings.TradingSettings.ConfirmTimeout)
		}
		if !ok {
			s.mu.Lock()
			s.cooldownTil[sig.InstID] = time.Now().Add(s.settings.TradingSettings.CooldownPerSymbol)
			s.mu.Unlock()
			s.setPending(sig.InstID, false)
			s.notifier.SendF(ctx, s.userID, "⛔️ [%s] Вход отменён/таймаут", sig.InstID)
			continue
		}

		// 3. Расчёт параметров сделки (твоя calcTradeParams, только на s.exch/s.settings)
		params, err := s.calcTradeParams(ctx, sig.InstID, string(sig.Side), sig.Price)
		if err != nil {
			s.notifier.SendF(ctx, s.userID,
				"❗️ [%s] Ошибка расчёта параметров сделки: %v", sig.InstID, err)
			s.setPending(sig.InstID, false)
			continue
		}

		// 4. PlaceMarket + PlaceTpsl через s.exch
		if err := s.openPositionWithTpSl(ctx, sig, params); err != nil {
			s.notifier.SendF(ctx, s.userID,
				"❗️ [%s] Ошибка открытия ордера: %v", sig.InstID, err)
			s.setPending(sig.InstID, false)
			continue
		}

		s.setPending(sig.InstID, false)
	}
}

func (s *userSession) setPending(symbol string, v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[symbol] = v
}

// openPositionWithTpSl открывает рыночный ордер и пытается поставить TP/SL.
// Возвращает orderID рыночного ордера (если успешно) или ошибку.
func (s *userSession) openPositionWithTpSl(
	ctx context.Context,
	sig models.Signal,
	params *TradeParams,
) error {

	// 1. Маппим сторону в OKX side/openType
	openType := 1 // 1 = open long/short
	var sideInt int
	switch strings.ToUpper(params.Direction) {
	case "BUY":
		sideInt = 1 // open long
	case "SELL":
		sideInt = 3 // open short
	default:
		return fmt.Errorf("unknown direction: %q", params.Direction)
	}

	// 2. Открываем рыночный ордер
	orderID, err := s.okx.PlaceMarket(
		ctx,
		sig.InstID,
		params.Size,
		sideInt,
		params.Leverage,
		openType,
	)
	if err != nil {
		return fmt.Errorf("PlaceMarket: %w", err)
	}

	// 3. TP/SL (order-algo)
	posSide := "long"
	if strings.EqualFold(params.Direction, "SELL") {
		posSide = "short"
	}

	// debug для себя
	s.notifier.SendF(ctx, s.userID,
		"[%s] DEBUG entry=%.6f SL=%.6f TP=%.6f 1R=%.6f RR=%.2f risk=%.2f%% size=%.4f (%s)",
		sig.InstID,
		params.Entry, params.SL, params.TP, params.RiskDist,
		params.RR, params.RiskPct, params.Size,
		sig.Reason,
	)

	// 1) Stop-loss
	err = s.okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.SL, false)
	if err != nil {
		s.notifier.SendF(ctx, s.userID,
			"⚠️ [%s] TP/SL не выставлены на OKX: %v", sig.InstID, err)
	}

	// 2) Take-profit
	err = s.okx.PlaceSingleAlgo(ctx, sig.InstID, posSide, params.Size, params.TP, true)
	if err != nil {
		s.notifier.SendF(ctx, s.userID,
			"⚠️ [%s] TP/SL не выставлены на OKX: %v", sig.InstID, err)

	}

	// 4. Финальное сообщение об успешном входе
	s.notifier.SendF(ctx,
		s.userID,
		"✅ [%s] Вход подтверждён | OPEN %-4s @ %.4f | SL=%.4f TP=%.4f lev=%dx size=%.4f | strategy=%s (orderId=%s)",
		sig.InstID,
		params.Direction,
		params.Entry,
		params.SL,
		params.TP,
		params.Leverage,
		params.Size,
		sig.Strategy,
		orderID,
	)

	return nil
}
func (s *userSession) Status(ctx context.Context) ([]models.OpenPosition, error) {
	// просто прокидываем в OKX-клиент, который уже сконфигурен под этого юзера
	positions, err := s.okx.OpenPositions(ctx)
	if err != nil {
		return nil, err
	}
	return positions, nil
}
