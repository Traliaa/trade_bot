package sessions

import (
	"context"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

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
}
