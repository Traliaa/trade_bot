package sessions

import (
	"context"
	"time"

	"go.uber.org/zap"
)

func (s *UserSession) TradeHistoryWorker(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.SyncClosedTrades(ctx); err != nil {
				s.Logger.Error("trade history sync failed", zap.Error(err))
			}
		}
	}
}
