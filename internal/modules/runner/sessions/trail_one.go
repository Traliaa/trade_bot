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
	s.PosMu.RLock()
	st := s.Positions[key]
	s.PosMu.RUnlock()
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

	// Решение только на 15m слот (даже если свеча 1m)
	dec := decideTrail15m(st, s.User.Settings, ct.End)
	if !dec.MoveSL && !dec.Close {
		return
	}

	// rate limit по времени (на всякий)
	if !st.LastTrailAt.IsZero() && ct.End.Sub(st.LastTrailAt) < 60*time.Second {
		return
	}
	// --- PARTIAL CLOSE ---
	if dec.CloseSize > 0 {
		_, _ = s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, dec.CloseSize)

		// уменьшаем локально size, чтобы дальше SL ставился на остаток
		s.PosMu.Lock()
		if st.Size > dec.CloseSize {
			st.Size -= dec.CloseSize
		} else {
			// если внезапно закрыли всё — удаляем стейт
			delete(s.Positions, key)
		}
		st.LastTrailAt = ct.End
		s.PosMu.Unlock()

		if s.canSend("partial:"+st.InstID+":"+st.PosSide, 30*time.Minute) {
			s.Notifier.SendF(ctx, s.User.TelegramID,
				"💰 [%s] Частичная фиксация (%s) закрыто=%.4f | %s",
				st.InstID, st.PosSide, dec.CloseSize, dec.Reason,
			)
		}
		return
	}
	// --- CLOSE ---
	if dec.Close {
		_, _ = s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, st.Size)

		// удаляем стейт, чтобы не трогать закрытую
		s.PosMu.Lock()
		delete(s.Positions, key)
		s.PosMu.Unlock()

		s.Notifier.SendF(ctx, s.User.TelegramID,
			"🕒 [%s] TimeStop закрытие позиции (%s) | reason=%s",
			st.InstID, st.PosSide, dec.Reason,
		)
		return
	}

	// --- MOVE SL ---
	newSL := dec.NewSL
	if st.TickSz > 0 {
		if st.PosSide == "long" {
			newSL = helper.RoundUpToTick(newSL, st.TickSz)
		} else {
			newSL = helper.RoundDownToTick(newSL, st.TickSz)
		}
	}

	// cancel old SL
	_ = s.Okx.CancelAlgo(ctx, st.InstID, st.AlgoID)

	// place new SL
	newAlgoID, err := s.Okx.PlaceSingleAlgo(ctx, st.InstID, st.PosSide, st.Size, newSL, false)
	if err != nil {
		return
	}

	s.PosMu.Lock()
	st.SL = newSL
	st.AlgoID = newAlgoID
	st.LastTrailAt = ct.End
	// LastTrailEnd уже выставил decideTrail15m через slot
	s.PosMu.Unlock()

	if s.canSend("trail:"+st.InstID+":"+st.PosSide, 15*time.Minute) {
		s.Notifier.SendF(ctx, s.User.TelegramID,
			"🛡 [%s] SL обновлён (%s) -> %.6f | %s",
			st.InstID, st.PosSide, newSL, dec.Reason,
		)
	}
}

func decideTrail15m(
	st *models.PositionTrailState,
	cfg models.Settings,
	slotEnd time.Time,
) models.TrailDecision {
	R := st.RiskDist
	if R <= 0 || st.Entry <= 0 || st.SL <= 0 {
		return models.TrailDecision{}
	}

	// 1 апдейт на 15m слот
	slot := helper.TrailSlot15m(slotEnd)
	if !st.LastTrailEnd.IsZero() && st.LastTrailEnd.Equal(slot) {
		return models.TrailDecision{}
	}

	// --- helper: улучшение SL ---
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

	// --- прогресс в R по MFE ---
	var mfeR float64
	if st.PosSide == "long" {
		mfeR = (st.MFE - st.Entry) / R
	} else {
		mfeR = (st.Entry - st.MFE) / R
	}

	// --- тайм-стоп: 3 часа и не дал 0.3R ---
	if cfg.TrailingConfig.TimeStopBars > 0 &&
		cfg.TrailingConfig.TimeStopMinMFER > 0 &&
		!st.OpenedAt.IsZero() {

		maxDur := time.Duration(cfg.TrailingConfig.TimeStopBars) * 15 * time.Minute
		if slotEnd.Sub(st.OpenedAt) >= maxDur && mfeR < cfg.TrailingConfig.TimeStopMinMFER {
			st.LastTrailEnd = slot
			return models.TrailDecision{
				Close:  true,
				Reason: "TIME_STOP",
			}
		}
	}

	// --- 1) BE на 0.6R ---
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
			return models.TrailDecision{NewSL: cand, MoveSL: true, Reason: "BE@0.6R"}
		}
	}

	// --- PARTIAL TAKE ---
	if cfg.TrailingConfig.PartialEnabled &&
		!st.TookPartial &&
		mfeR >= cfg.TrailingConfig.PartialTriggerR &&
		st.Size > 0 {

		closeSz := st.Size * cfg.TrailingConfig.PartialCloseFrac
		if closeSz > 0 {
			st.TookPartial = true
			st.LastTrailEnd = slot

			return models.TrailDecision{
				CloseSize: closeSz,
				Reason: fmt.Sprintf(
					"PARTIAL@%.2fR (%.0f%%)",
					cfg.TrailingConfig.PartialTriggerR,
					cfg.TrailingConfig.PartialCloseFrac*100,
				),
			}
		}
	}
	// --- 2) Lock profit на 0.9R: SL = Entry + 0.3R ---
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
			return models.TrailDecision{NewSL: cand, MoveSL: true, Reason: "LOCK@0.9R->0.3R"}
		}
	}

	// ничего не делаем, но слот фиксировать не нужно
	return models.TrailDecision{}
}
