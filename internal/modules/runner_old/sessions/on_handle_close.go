package sessions

import (
	"context"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"

	"go.uber.org/zap"
)

var _ = zap.String // keep zap import for Logger.Warn

func (s *UserSession) OnCandleClose(ctx context.Context, ct models.CandleTick) {
	if helper.NormTF(ct.TimeframeRaw) != "1m" {
		return
	}

	s.ExchangeMu.RLock()
	pLong, okLong := s.ExchangePositions[models.PosKey{InstID: ct.InstID, PosSide: "long"}]
	pShort, okShort := s.ExchangePositions[models.PosKey{InstID: ct.InstID, PosSide: "short"}]
	s.ExchangeMu.RUnlock()

	if okLong {
		s.trailOne(ctx, ct, pLong)
	}
	if okShort {
		s.trailOne(ctx, ct, pShort)
	}

	// V3 partial close: если V3-стратегия отметила PartialDone, а runner ещё не обработал — выполняем.
	if s.V3PartialCheck != nil && s.V3PartialCheck(ct.InstID) {
		s.checkV3PartialForSide(ctx, ct, "long")
		s.checkV3PartialForSide(ctx, ct, "short")
	}
}

// checkV3PartialForSide проверяет и выполняет частичное закрытие по сигналу V3.
func (s *UserSession) checkV3PartialForSide(ctx context.Context, ct models.CandleTick, posSide string) {
	key := models.PosKey{InstID: ct.InstID, PosSide: posSide}

	s.TrailMu.Lock()
	st, ok := s.TrailStates[key]
	if !ok || st == nil || st.TookPartial {
		s.TrailMu.Unlock()
		return
	}

	cfg := s.SettingsSnapshot().TrailingConfig
	if !cfg.PartialEnabled {
		s.TrailMu.Unlock()
		return
	}

	closeSz := st.Size * cfg.PartialCloseFrac
	if closeSz <= 0 {
		s.TrailMu.Unlock()
		return
	}
	s.TrailMu.Unlock()

	dec := closeSz // closeSz = amount to close

	_, err := s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, dec)
	if err != nil {
		s.Logger.Warn("v3 partial close failed",
			zap.Error(err),
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
			zap.Float64("closeSize", dec),
		)
		return
	}

	s.TrailMu.Lock()
	if st.Size > dec {
		st.Size -= dec
		st.TookPartial = true
		st.LastTrailAt = ct.End
	} else {
		delete(s.TrailStates, key)
		s.TrailMu.Unlock()
		return
	}
	newSize := st.Size
	s.TrailMu.Unlock()

	if err := s.syncTradeFlagsFromState(ctx, st, newSize); err != nil {
		s.Logger.Warn("v3 partial sync flag failed",
			zap.Error(err),
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
		)
	}
}
