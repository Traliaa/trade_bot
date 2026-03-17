package sessions

import (
	"context"
	"fmt"

	"time"

	"trade_bot/internal/helper"
	"trade_bot/internal/models"

	"go.uber.org/zap"
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
	s.TrailMu.Lock()
	if p.Size > 0 {
		st.Size = p.Size
	}
	if p.Entry > 0 {
		st.Entry = p.Entry
	}
	// 1m MFE update
	st.UpdateMFE(ct.High, ct.Low)
	s.TrailMu.Unlock()

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

		oldSize := st.Size

		_, err := s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, dec.CloseSize)
		if err != nil {
			s.Logger.Warn("partial close failed",
				zap.Error(err),
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
				zap.Float64("closeSize", dec.CloseSize),
			)
			return
		}

		s.TrailMu.Lock()
		if st.Size > dec.CloseSize {
			st.Size -= dec.CloseSize
			st.TookPartial = true
			st.LastTrailAt = ct.End
		} else {
			// Если фактически закрыли всё, удаляем state и выходим
			delete(s.TrailStates, key)
			s.TrailMu.Unlock()

			s.Logger.Info("partial close consumed full size",
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
				zap.Float64("oldSize", oldSize),
				zap.Float64("closeSize", dec.CloseSize),
			)
			return
		}
		newSize := st.Size
		s.TrailMu.Unlock()

		if err := s.syncTradeFlagsFromState(ctx, st, newSize); err != nil {
			s.Logger.Warn("sync partial flag failed",
				zap.Error(err),
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
			)
		}

		// После partial сразу защищаем остаток позиции
		if dec.MoveSLAfterPartial && st.AlgoID != "" && newSize > 0 {
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

				newAlgoID, err := s.Okx.PlaceSingleAlgo(ctx, st.InstID, st.PosSide, newSize, newSL, false)
				if err != nil {
					s.Logger.Warn("place post-partial SL failed",
						zap.Error(err),
						zap.String("instId", st.InstID),
						zap.String("posSide", st.PosSide),
						zap.Float64("newSL", newSL),
						zap.Float64("size", newSize),
					)
				} else {
					s.TrailMu.Lock()
					st.SL = newSL
					st.AlgoID = newAlgoID
					// после успешного сдвига стопа к BE считаем это moved to BE
					if approxAtOrBeyondBE(st, newSL) {
						st.MovedToBE = true
					}
					st.LastTrailAt = ct.End
					s.TrailMu.Unlock()

					if err := s.syncTradeFlagsFromState(ctx, st, st.Size); err != nil {
						s.Logger.Warn("sync post-partial SL flag failed",
							zap.Error(err),
							zap.String("instId", st.InstID),
							zap.String("posSide", st.PosSide),
						)
					}
				}
			}
		}

		s.Logger.Info("partial executed",
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
			zap.Float64("oldSize", oldSize),
			zap.Float64("newSize", newSize),
			zap.Float64("closeSize", dec.CloseSize),
		)

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

		if err := s.syncTradeCloseIntent(ctx, st, dec.Reason); err != nil {
			s.Logger.Warn("sync close intent failed",
				zap.Error(err),
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
			)
		}

		_, err := s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, st.Size)
		if err != nil {
			s.Logger.Warn("close market failed",
				zap.Error(err),
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
				zap.Float64("size", st.Size),
			)
			return
		}

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
		s.Logger.Warn("move SL failed",
			zap.Error(err),
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
			zap.Float64("newSL", newSL),
			zap.Float64("size", st.Size),
		)
		return
	}

	s.TrailMu.Lock()
	prevSL := st.SL
	st.SL = newSL
	st.AlgoID = newAlgoID
	st.LastTrailAt = ct.End

	// Флаги выставляем после успешного исполнения
	if !st.MovedToBE && approxAtOrBeyondBE(st, newSL) {
		st.MovedToBE = true
	}
	if !st.LockedProfit && approxLocksProfit(st, prevSL, newSL) {
		st.LockedProfit = true
	}

	s.TrailMu.Unlock()

	if err := s.syncTradeFlagsFromState(ctx, st, st.Size); err != nil {
		s.Logger.Warn("sync move SL flag failed",
			zap.Error(err),
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
		)
	}

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

func approxAtOrBeyondBE(st *models.PositionTrailState, sl float64) bool {
	if st == nil || sl <= 0 || st.Entry <= 0 {
		return false
	}

	const eps = 1e-12

	if st.PosSide == "long" {
		return sl >= st.Entry-eps
	}
	return sl <= st.Entry+eps
}

func approxLocksProfit(st *models.PositionTrailState, prevSL, newSL float64) bool {
	if st == nil || newSL <= 0 || st.Entry <= 0 {
		return false
	}

	switch st.PosSide {
	case "long":
		return newSL > st.Entry && newSL > prevSL
	case "short":
		return newSL < st.Entry && newSL < prevSL
	default:
		return false
	}
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
			return models.TrailDecision{
				Close:  true,
				Reason: models.CloseReasonTimeStopEarly,
				Note:   "TIME_STOP_EARLY",
			}
		}
	}

	// --- STALE EXIT ---
	if cfg.TrailingConfig.TimeStopBars > 0 && !st.OpenedAt.IsZero() {
		maxDur := time.Duration(cfg.TrailingConfig.TimeStopBars) * 15 * time.Minute
		if slotEnd.Sub(st.OpenedAt) >= maxDur &&
			currentR < cfg.TrailingConfig.TimeStopMinCurrentR {
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
			return models.TrailDecision{
				NewSL:  cand,
				MoveSL: true,
				Reason: models.CloseReasonBreakEven,
				Note:   "BE",
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
			return models.TrailDecision{
				NewSL:  cand,
				MoveSL: true,
				Reason: models.CloseReasonLockProfit,
				Note:   "LOCK",
			}
		}
	}

	return models.TrailDecision{}
}
func (s *UserSession) syncTradePayloadFromTrail(
	ctx context.Context,
	instID string,
	posSide string,
	patch func(*models.TradePayload),
) error {
	tr, err := s.Repo.FindOpenTrade(ctx, s.User.TelegramID, instID)
	if err != nil {
		return err
	}
	if tr == nil {
		return nil
	}
	if tr.Payload.PosSide != posSide {
		return nil
	}

	payload := tr.Payload
	patch(&payload)

	return s.Repo.UpdatePayload(ctx, tr.GUID, payload)
}

func (s *UserSession) syncTradeFlagsFromState(
	ctx context.Context,
	st *models.PositionTrailState,
	currentSize float64,
) error {
	if st == nil {
		return nil
	}

	return s.syncTradePayloadFromTrail(ctx, st.InstID, st.PosSide, func(p *models.TradePayload) {
		p.MovedToBE = st.MovedToBE
		p.LockedProfit = st.LockedProfit
		p.TookPartial = st.TookPartial

		if currentSize > 0 {
			p.CurrentSize = currentSize
			if p.EntrySize > 0 && currentSize < p.EntrySize {
				p.TookPartial = true
				p.ClosedSize = p.EntrySize - currentSize
				if p.PartialCount == 0 {
					p.PartialCount = 1
				}
			}
		}
	})
}
