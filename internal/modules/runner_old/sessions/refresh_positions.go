package sessions

import (
	"context"
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

	s.ExchangeMu.Lock()
	s.ExchangePositions = next
	s.ExchangePositionsAt = now
	s.ExchangeMu.Unlock()

	// подчистим трейл-стейт для закрытых позиций
	s.TrailMu.Lock()
	for key := range s.TrailStates {
		inst, side := key.InstID, key.PosSide
		if _, ok := next[models.PosKey{InstID: inst, PosSide: side}]; !ok {
			delete(s.TrailStates, key)
		}
	}
	s.TrailMu.Unlock()

	return nil
}
