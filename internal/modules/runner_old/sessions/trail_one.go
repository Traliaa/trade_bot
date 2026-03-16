package sessions

import (
	"context"
	"fmt"

	"time"

	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

const (
	minTrailGap = 3 * time.Minute
	minImproveR = 0.10
)

func (s *UserSession) trailOne(ctx context.Context, ct models.CandleTick, p models.CachedPos) {
	key := helper.TrailKey(ct.InstID, p.PosSide)

	// trail state
	s.TrailMu.RLock()
	st := s.TrailStates[key]
	s.TrailMu.RUnlock()
	if st == nil || st.AlgoID == "" || st.RiskDist <= 0 {
		return
	}

	// sync from cache
	if p.Size > 0 {
		st.Size = p.Size
	}
	if p.Entry > 0 {
		st.Entry = p.Entry
	}

	// 1m MFE update
	st.UpdateMFE(ct.High, ct.Low)

	// Решение только на 15m слот
	dec := decideTrail15m(st, s.User.Settings, ct.Close, ct.End)
	if !dec.MoveSL && !dec.Close && dec.CloseSize <= 0 {
		return
	}

	// rate limit по времени
	if !st.LastTrailAt.IsZero() && ct.End.Sub(st.LastTrailAt) < 60*time.Second {
		return
	}

	// --- PARTIAL CLOSE ---
	if dec.CloseSize > 0 {
		if dec.CloseSize <= 0 || st.Size <= 0 {
			return
		}

		_, _ = s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, dec.CloseSize)

		s.TrailMu.Lock()
		if st.Size > dec.CloseSize {
			st.Size -= dec.CloseSize
		} else {
			delete(s.TrailStates, key)
			s.TrailMu.Unlock()
			return
		}
		st.LastTrailAt = ct.End
		s.TrailMu.Unlock()

		// После partial сразу защищаем остаток позиции
		if dec.MoveSLAfterPartial && st.AlgoID != "" && st.Size > 0 {
			newSL := dec.NewSLAfterPartial

			if shouldImproveSL(st, newSL) {
				if st.TickSz > 0 {
					if st.PosSide == "long" {
						newSL = helper.RoundUpToTick(newSL, st.TickSz)
					} else {
						newSL = helper.RoundDownToTick(newSL, st.TickSz)
					}
				}

				_ = s.Okx.CancelAlgo(ctx, st.InstID, st.AlgoID)

				newAlgoID, err := s.Okx.PlaceSingleAlgo(ctx, st.InstID, st.PosSide, st.Size, newSL, false)
				if err == nil {
					s.TrailMu.Lock()
					st.SL = newSL
					st.AlgoID = newAlgoID
					s.TrailMu.Unlock()
				}
			}
		}

		if s.canSend("partial:"+st.InstID+":"+st.PosSide, 30*time.Minute) {
			msg := dec.Note
			if msg == "" {
				msg = string(dec.Reason)
			}

			s.Notifier.SendF(ctx, s.User.TelegramID,
				"💰 [%s] Частичная фиксация (%s) закрыто=%.4f | %s",
				st.InstID, st.PosSide, dec.CloseSize, msg,
			)
		}
		return
	}

	// --- CLOSE ---
	if dec.Close {
		if st.Size <= 0 {
			return
		}

		now := time.Now()

		s.TrailMu.Lock()
		st.CloseReason = dec.Reason
		st.ClosingAt = &now
		s.TrailMu.Unlock()

		_, _ = s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, st.Size)

		s.TrailMu.Lock()
		delete(s.TrailStates, key)
		s.TrailMu.Unlock()

		msg := dec.Note
		if msg == "" {
			msg = string(dec.Reason)
		}

		s.Notifier.SendF(ctx, s.User.TelegramID,
			"🕒 [%s] Закрытие позиции (%s) | reason=%s",
			st.InstID, st.PosSide, msg,
		)
		return
	}

	// --- MOVE SL ---
	newSL := dec.NewSL
	if !shouldImproveSL(st, newSL) {
		return
	}

	if st.TickSz > 0 {
		if st.PosSide == "long" {
			newSL = helper.RoundUpToTick(newSL, st.TickSz)
		} else {
			newSL = helper.RoundDownToTick(newSL, st.TickSz)
		}
	}

	_ = s.Okx.CancelAlgo(ctx, st.InstID, st.AlgoID)

	newAlgoID, err := s.Okx.PlaceSingleAlgo(ctx, st.InstID, st.PosSide, st.Size, newSL, false)
	if err != nil {
		return
	}

	s.TrailMu.Lock()
	st.SL = newSL
	st.AlgoID = newAlgoID
	st.LastTrailAt = ct.End
	s.TrailMu.Unlock()

	if s.canSend("trail:"+st.InstID+":"+st.PosSide, 15*time.Minute) {
		msg := dec.Note
		if msg == "" {
			msg = string(dec.Reason)
		}

		s.Notifier.SendF(ctx, s.User.TelegramID,
			"🛡 [%s] SL обновлён (%s) -> %.6f | %s",
			st.InstID, st.PosSide, newSL, msg,
		)
	}
}

func shouldImproveSL(st *models.PositionTrailState, candidate float64) bool {
	if st == nil || candidate <= 0 || st.SL <= 0 {
		return false
	}

	if st.PosSide == "long" {
		return candidate > st.SL
	}

	return candidate < st.SL
}
func decideTrail15m(
	st *models.PositionTrailState,
	cfg models.Settings,
	lastPrice float64,
	slotEnd time.Time,
) models.TrailDecision {
	R := st.RiskDist
	if R <= 0 || st.Entry <= 0 || st.SL <= 0 {
		return models.TrailDecision{}
	}

	slot := helper.TrailSlot15m(slotEnd)
	if !st.LastTrailEnd.IsZero() && st.LastTrailEnd.Equal(slot) {
		return models.TrailDecision{}
	}

	improves := func(candidate float64) bool {
		if st.PosSide == "long" {
			return candidate > st.SL
		}
		return candidate < st.SL
	}

	minImprove := 0.10 * R
	improvesEnough := func(candidate float64) bool {
		if st.PosSide == "long" {
			return candidate-st.SL >= minImprove
		}
		return st.SL-candidate >= minImprove
	}

	var mfeR float64
	if st.PosSide == "long" {
		mfeR = (st.MFE - st.Entry) / R
	} else {
		mfeR = (st.Entry - st.MFE) / R
	}

	var currentR float64
	if lastPrice > 0 {
		if st.PosSide == "long" {
			currentR = (lastPrice - st.Entry) / R
		} else {
			currentR = (st.Entry - lastPrice) / R
		}
	}

	// --- EARLY FAIL ---
	if cfg.TrailingConfig.EarlyTimeStopBars > 0 &&
		cfg.TrailingConfig.EarlyTimeStopMinMFER > 0 &&
		!st.OpenedAt.IsZero() {

		earlyDur := time.Duration(cfg.TrailingConfig.EarlyTimeStopBars) * 15 * time.Minute
		if slotEnd.Sub(st.OpenedAt) >= earlyDur && mfeR < cfg.TrailingConfig.EarlyTimeStopMinMFER {
			st.LastTrailEnd = slot
			return models.TrailDecision{
				Close:  true,
				Reason: models.CloseReasonTimeStopEarly,
				Note:   "TIME_STOP_EARLY",
			}
		}
	}

	// --- STALE EXIT ---
	if cfg.TrailingConfig.TimeStopBars > 0 &&
		!st.OpenedAt.IsZero() {

		maxDur := time.Duration(cfg.TrailingConfig.TimeStopBars) * 15 * time.Minute
		if slotEnd.Sub(st.OpenedAt) >= maxDur &&
			currentR < cfg.TrailingConfig.TimeStopMinCurrentR {
			st.LastTrailEnd = slot
			return models.TrailDecision{
				Close:  true,
				Reason: models.CloseReasonTimeStopStale,
				Note:   "TIME_STOP_STALE",
			}
		}
	}

	// --- BE ---
	if !st.MovedToBE && mfeR >= cfg.TrailingConfig.BETriggerR {
		cand := st.Entry
		if cfg.TrailingConfig.BEOffsetR != 0 {
			if st.PosSide == "long" {
				cand = st.Entry + cfg.TrailingConfig.BEOffsetR*R
			} else {
				cand = st.Entry - cfg.TrailingConfig.BEOffsetR*R
			}
		}

		if improves(cand) && improvesEnough(cand) {
			st.MovedToBE = true
			st.LastTrailEnd = slot
			return models.TrailDecision{
				NewSL:  cand,
				MoveSL: true,
				Reason: models.CloseReasonUnknown,
				Note:   "BE@0.6R",
			}
		}
	}

	// --- PARTIAL ---
	if cfg.TrailingConfig.PartialEnabled &&
		!st.TookPartial &&
		mfeR >= cfg.TrailingConfig.PartialTriggerR &&
		st.Size > 0 {

		closeSz := st.Size * cfg.TrailingConfig.PartialCloseFrac
		if closeSz > 0 {
			st.TookPartial = true
			st.LastTrailEnd = slot

			newSL := st.Entry
			if cfg.TrailingConfig.BEOffsetR != 0 {
				if st.PosSide == "long" {
					newSL = st.Entry + cfg.TrailingConfig.BEOffsetR*R
				} else {
					newSL = st.Entry - cfg.TrailingConfig.BEOffsetR*R
				}
			}

			return models.TrailDecision{
				CloseSize: closeSz,
				Reason:    models.CloseReasonPartialExit,
				Note: fmt.Sprintf(
					"PARTIAL@%.2fR (%.0f%%) + SL->BE",
					cfg.TrailingConfig.PartialTriggerR,
					cfg.TrailingConfig.PartialCloseFrac*100,
				),
				MoveSLAfterPartial: true,
				NewSLAfterPartial:  newSL,
			}
		}
	}

	// --- LOCK ---
	if !st.LockedProfit && mfeR >= cfg.TrailingConfig.LockTriggerR {
		var cand float64
		if st.PosSide == "long" {
			cand = st.Entry + cfg.TrailingConfig.LockOffsetR*R
		} else {
			cand = st.Entry - cfg.TrailingConfig.LockOffsetR*R
		}

		if improves(cand) && improvesEnough(cand) {
			st.LockedProfit = true
			st.LastTrailEnd = slot
			return models.TrailDecision{
				NewSL:  cand,
				MoveSL: true,
				Reason: models.CloseReasonUnknown,
				Note:   "LOCK@0.9R->0.3R",
			}
		}
	}

	return models.TrailDecision{}
}
