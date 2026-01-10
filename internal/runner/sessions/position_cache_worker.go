package sessions

import (
	"context"

	"time"
)

func (s *UserSession) PositionCacheWorker(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	_ = s.RefreshPositions(ctx) // сразу при старте

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.RefreshPositions(ctx)
		}
	}
}
