package service

import (
	"context"
	"sync"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/runner/sessions"

	"github.com/bytedance/gopkg/util/logger"
	"go.uber.org/zap"
)

type candleAgg struct {
	mu   sync.Mutex
	last map[string]models.CandleTick // instId -> last 1m candle
}

func NewCandleAgg() *candleAgg {
	return &candleAgg{last: make(map[string]models.CandleTick)}
}

func (a *candleAgg) Put(ct models.CandleTick) {
	a.mu.Lock()
	a.last[ct.InstID] = ct
	a.mu.Unlock()
}

func (a *candleAgg) Drain() []models.CandleTick {
	a.mu.Lock()
	out := make([]models.CandleTick, 0, len(a.last))
	for _, ct := range a.last {
		out = append(out, ct)
	}
	// очищаем — следующий тик перезапишет
	a.last = make(map[string]models.CandleTick)
	a.mu.Unlock()
	return out
}

var sem = make(chan struct{}, 4) // максимум 4 HTTP одновременно

func (r *Service) OnCandleClose(ctx context.Context, ct models.CandleTick) {
	if helper.NormTF(ct.TimeframeRaw) != "1m" {
		return
	}

	r.mu.RLock()
	uS := make([]*sessions.UserSession, 0, len(r.users))
	for _, s := range r.users {
		uS = append(uS, s)
	}
	r.mu.RUnlock()

	for _, sess := range uS {
		s := sess

		select {
		case sem <- struct{}{}:
			go func() {
				defer func() { <-sem }()
				s.OnCandleClose(ctx, ct)
			}()
		case <-time.After(300 * time.Millisecond):
			logger.Warn("пропуск свечи: заняты воркеры", zap.String("inst", ct.InstID))
		case <-ctx.Done():
			return
		}
	}
}
