package sessions

import (
	"context"
	"time"

	"trade_bot/internal/models"

	"go.uber.org/zap"
)

func (s *UserSession) RestoreTrailStates(ctx context.Context) error {
	openTrades, err := s.Repo.ListOpenTrades(ctx, s.User.TelegramID)
	if err != nil {
		return err
	}

	restored := make(map[models.PosKey]*models.PositionTrailState, len(openTrades))

	for _, tr := range openTrades {
		st := trailStateFromTrade(tr)
		if st == nil {
			continue
		}

		key := models.PosKey{
			InstID:  st.InstID,
			PosSide: st.PosSide,
		}
		restored[key] = st
	}

	s.TrailMu.Lock()
	if s.TrailStates == nil {
		s.TrailStates = make(map[models.PosKey]*models.PositionTrailState)
	}
	for k, v := range restored {
		s.TrailStates[k] = v
	}
	s.TrailMu.Unlock()

	s.Logger.Info("trail states restored",
		zap.Int64("userID", s.User.TelegramID),
		zap.Int("count", len(restored)),
	)

	return nil
}
func trailStateFromTrade(tr models.TradeRecord) *models.PositionTrailState {
	p := tr.Payload
	if p.PosSide == "" || p.EntryPrice <= 0 || p.StopLoss <= 0 || p.EntrySize <= 0 {
		return nil
	}

	size := p.CurrentSize
	if size <= 0 {
		size = p.EntrySize
	}

	st := &models.PositionTrailState{
		InstID:       tr.InstID,
		PosSide:      p.PosSide,
		Entry:        p.EntryPrice,
		SL:           p.StopLoss,
		TP:           p.TakeProfit,
		RiskDist:     p.RiskDist,
		AlgoID:       p.AlgoID,
		Size:         size,
		MovedToBE:    p.MovedToBE,
		LockedProfit: p.LockedProfit,
		TookPartial:  p.TookPartial,
		OpenedAt:     tr.EntryAt,
	}

	if st.RiskDist <= 0 {
		st.RiskDist = models.CalcRiskDist(p.EntryPrice, p.StopLoss, p.PosSide)
	}

	// восстановим MFE из payload, если был
	if p.MFEPrice > 0 {
		st.MFE = p.MFEPrice
	} else {
		st.MFE = p.EntryPrice
	}

	// чтобы не словить лишний трейл сразу после рестарта
	st.LastTrailAt = time.Time{}
	st.LastTrailEnd = time.Time{}

	return st
}
