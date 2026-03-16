package sessions

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/models"

	"go.uber.org/zap"
)

func (s *UserSession) StartAccountRefresher(parent context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-parent.Done():
				return
			case <-s.stopCh:
				return
			case <-ticker.C:
				ctx, cancel := context.WithTimeout(parent, 10*time.Second)
				err := s.RefreshAccountSnapshot(ctx)
				cancel()

				if err != nil {
					s.Logger.Warn("background account snapshot refresh failed",
						zap.Error(err),
						zap.Int64("userID", s.User.TelegramID),
					)
				}
			}
		}
	}()
}

func (s *UserSession) AccountSnapshot() models.AccountSnapshot {
	return s.User.AccountSnapshot
}

func (s *UserSession) SetAccountSnapshot(snap models.AccountSnapshot) {
	s.User.AccountSnapshot = snap
}

func (s *UserSession) RefreshAccountSnapshot(ctx context.Context) error {
	snap, err := s.Okx.USDTBalance(ctx)
	if err != nil {
		return err
	}
	if snap == nil {
		return fmt.Errorf("account snapshot is nil")
	}

	s.SetAccountSnapshot(*snap)

	s.Logger.Debug("account snapshot refreshed",
		zap.Int64("userID", s.User.TelegramID),
		zap.Float64("totalEquity", snap.TotalEquity),
		zap.Float64("availableBalance", snap.AvailableBalance),
		zap.Float64("unrealizedPnL", snap.UnrealizedPnL),
	)

	return nil
}

func (s *UserSession) RiskEquity() float64 {
	snap := s.AccountSnapshot()

	if snap.AvailableBalance > 0 {
		return snap.AvailableBalance
	}
	if snap.TotalEquity > 0 {
		return snap.TotalEquity
	}

	return 0
}
