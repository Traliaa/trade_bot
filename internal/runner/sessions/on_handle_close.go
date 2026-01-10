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

	s.PosCacheMu.RLock()
	pLong, okLong := s.PositionsCache[models.PosKey{InstID: ct.InstID, PosSide: "long"}]
	pShort, okShort := s.PositionsCache[models.PosKey{InstID: ct.InstID, PosSide: "short"}]
	s.PosCacheMu.RUnlock()

	if okLong {
		s.trailOne(ctx, ct, pLong)
	}
	if okShort {
		s.trailOne(ctx, ct, pShort)
	}
}
