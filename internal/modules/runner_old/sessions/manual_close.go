package sessions

import (
	"context"
	"time"
	"trade_bot/internal/models"
)

// Refresh only from an exchange position snapshot, never from requested size.
func (s *UserSession) ApplyManualPositionSnapshot(instID, side string, size float64) {
	s.TrailExecMu.Lock()
	defer s.TrailExecMu.Unlock()
	s.applyManualPositionSnapshot(instID, side, size)
}

// Read and apply under the same execution lock as automated protection.
func (s *UserSession) RefreshManualPositionSnapshot(ctx context.Context, instID, side string) (float64, error) {
	s.TrailExecMu.Lock()
	defer s.TrailExecMu.Unlock()
	size, err := s.Okx.ClosingPosition(ctx, instID, side)
	if err != nil {
		return 0, err
	}
	s.applyManualPositionSnapshot(instID, side, size)
	return size, nil
}

func (s *UserSession) applyManualPositionSnapshot(instID, side string, size float64) {
	key := models.PosKey{InstID: instID, PosSide: side}
	s.ExchangeMu.Lock()
	if size <= 0 {
		delete(s.ExchangePositions, key)
	} else if p, ok := s.ExchangePositions[key]; ok {
		p.Size = size
		s.ExchangePositions[key] = p
	}
	s.ExchangeMu.Unlock()
	s.TrailMu.Lock()
	defer s.TrailMu.Unlock()
	if size <= 0 {
		delete(s.TrailStates, key)
		return
	}
	if st := s.TrailStates[key]; st != nil {
		if size < st.Size {
			st.TookPartial = true
		}
		st.Size = size
		st.LastTrailAt = time.Now()
	}
}
