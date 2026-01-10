package sessions

import (
	"context"
	"time"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

const (
	minTrailGap = 3 * time.Minute
	minImproveR = 0.10
)

func (s *UserSession) trailOne(ctx context.Context, ct models.CandleTick, p models.CachedPos) {
	key := ct.InstID + ":" + p.PosSide

	// берём трейл-стейт
	s.PosMu.RLock()
	st := s.Positions[key]
	s.PosMu.RUnlock()

	if st == nil || st.RiskDist <= 0 || st.SL <= 0 || st.AlgoID == "" {
		return
	}

	// синхронизируем с кешем
	if p.Size > 0 {
		st.Size = p.Size
	}
	if p.Entry > 0 {
		st.Entry = p.Entry
	}

	// rate-limit
	if !st.LastTrailAt.IsZero() && ct.End.Sub(st.LastTrailAt) < minTrailGap {
		return
	}

	// считаем новый SL
	newSL, ok := st.MaybeTrailOnClosedCandle(ct.High, ct.Low, ct.End)
	if !ok {
		return
	}

	// минимальный шаг 0.1R
	minImprove := minImproveR * st.RiskDist
	if !improvesEnough(st.SL, newSL, st.PosSide, minImprove) {
		return
	}

	// округление по tick
	if st.TickSz > 0 {
		if st.PosSide == "long" {
			newSL = helper.RoundUpToTick(newSL, st.TickSz)
		} else {
			newSL = helper.RoundDownToTick(newSL, st.TickSz)
		}
	}

	// применяем SL
	_ = s.Okx.CancelAlgo(ctx, st.InstID, st.AlgoID)
	s.Notifier.SendF(ctx, s.UserID,
		"🧲 [%s] TRAIL %s mfe=%.6f entry=%.6f oldSL=%.6f -> newSL=%.6f (R=%.6f)",
		st.InstID, st.PosSide, st.MFE, st.Entry, st.SL, newSL, st.RiskDist,
	)
	newAlgoID, err := s.Okx.PlaceSingleAlgo(
		ctx, st.InstID, st.PosSide, st.Size, newSL, false,
	)
	if err != nil {
		return
	}
	s.Notifier.SendF(ctx, s.UserID,
		"✅ [%s] TRAIL OK sl=%.6f algoId=%s", st.InstID, st.SL, st.AlgoID,
	)

	// обновляем трейл-стейт
	s.PosMu.Lock()
	st.SL = newSL
	st.AlgoID = newAlgoID
	st.LastTrailAt = ct.End
	st.LastTrailEnd = ct.End
	s.PosMu.Unlock()
}
