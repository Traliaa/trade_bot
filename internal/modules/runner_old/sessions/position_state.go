package sessions

import (
	"time"
	"trade_bot/internal/models"
)

func (s *UserSession) getPositionState(instID, posSide string) (*models.PositionTrailState, bool) {
	key := instID + ":" + posSide

	s.PosMu.Lock()
	defer s.PosMu.Unlock()

	st, ok := s.Positions[key]
	return st, ok
}

func (s *UserSession) markPositionClosingReason(instID, posSide string, reason models.CloseReason) {
	key := instID + ":" + posSide

	s.PosMu.Lock()
	defer s.PosMu.Unlock()

	st, ok := s.Positions[key]
	if !ok || st == nil {
		return
	}

	now := time.Now()
	st.CloseReason = reason
	st.ClosingAt = &now
}
