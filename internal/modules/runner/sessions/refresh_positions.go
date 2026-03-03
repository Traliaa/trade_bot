package sessions

import (
	"context"
	"trade_bot/internal/helper"

	"time"
	"trade_bot/internal/models"
)

func (s *UserSession) RefreshPositions(ctx context.Context) error {
	positions, err := s.Okx.OpenPositions(ctx)
	if err != nil {
		return err
	}

	next := make(map[models.PosKey]models.CachedPos, len(positions))
	for _, p := range positions {
		if p.HoldVol <= 0 {
			continue
		}
		k := models.PosKey{InstID: p.Symbol, PosSide: p.Side}
		next[k] = models.CachedPos{
			InstID:  p.Symbol,
			PosSide: p.Side,
			Size:    p.HoldVol,
			Entry:   p.EntryPrice,
			LastPx:  p.LastPrice,
		}
	}

	now := time.Now()

	s.PosCacheMu.Lock()
	s.PositionsCache = next
	s.PosCacheAt = now
	s.PosCacheMu.Unlock()

	// подчистим трейл-стейт для закрытых позиций
	s.PosMu.Lock()
	for key := range s.Positions {
		inst, side, ok := helper.SplitTrailKey(key)
		if !ok {
			delete(s.Positions, key)
			continue
		}
		if _, ok := next[models.PosKey{InstID: inst, PosSide: side}]; !ok {
			delete(s.Positions, key)
		}
	}
	s.PosMu.Unlock()

	return nil
}
