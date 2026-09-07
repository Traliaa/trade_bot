package sessions

import (
	"time"
	"trade_bot/internal/models"
)

// Refresh only from an exchange position snapshot, never from requested size.
func (s *UserSession) ApplyManualPositionSnapshot(instID, side string, size float64) {
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
