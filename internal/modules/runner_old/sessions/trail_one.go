package sessions

import (
	"context"

	"time"

	"trade_bot/internal/helper"
	"trade_bot/internal/models"

	"go.uber.org/zap"
)

func (s *UserSession) trailOne(ctx context.Context, ct models.CandleTick, p models.CachedPos) {
	key := helper.TrailKey(ct.InstID, p.PosSide)

	s.TrailMu.RLock()
	st := s.TrailStates[key]
	s.TrailMu.RUnlock()
	if st == nil || st.AlgoID == "" || st.RiskDist <= 0 {
		return
	}

	var (
		dec         models.TrailDecision
		currentSize float64
	)

	// decideTrail15m теперь не pure: он может пометить позицию stale.
	s.TrailMu.Lock()
	if p.Size > 0 {
		st.Size = p.Size
	}
	if p.Entry > 0 {
		st.Entry = p.Entry
	}
	st.UpdateMFE(ct.High, ct.Low)

	dec = decideTrail15m(st, s.User.Settings, ct.Close, ct.End)
	currentSize = st.Size
	s.TrailMu.Unlock()

	// Даже если торгового действия нет, stale/BE/partial флаги могут измениться.
	if err := s.syncTradeFlagsFromState(ctx, st, currentSize); err != nil {
		s.Logger.Warn("sync trail state failed",
			zap.Error(err),
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
		)
	}

	if !dec.MoveSL && !dec.Close && dec.CloseSize <= 0 {
		return
	}

	// Дополнительный rate limit
	if !st.LastTrailAt.IsZero() && ct.End.Sub(st.LastTrailAt) < 60*time.Second {
		return
	}

	slot := helper.TrailSlot15m(ct.End)

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
			st.LastTrailEnd = slot
		} else {
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

		// После partial защищаем остаток
		if dec.MoveSLAfterPartial && st.AlgoID != "" && newSize > 0 {
			newSL := dec.NewSLAfterPartial
			if newSL <= 0 {
				newSL = calcBEPrice(st, s.User.Settings)
			}

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
					if approxAtOrBeyondBE(st, s.User.Settings, newSL) {
						st.MovedToBE = true
					}
					st.LastTrailAt = ct.End
					st.LastTrailEnd = slot
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
			zap.Bool("isStale", st.IsStale),
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
		st.LastTrailAt = ct.End
		st.LastTrailEnd = slot
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
				zap.String("reason", string(dec.Reason)),
				zap.String("note", dec.Note),
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
			zap.String("reason", string(dec.Reason)),
			zap.String("note", dec.Note),
		)
		return
	}

	s.TrailMu.Lock()
	prevSL := st.SL
	st.SL = newSL
	st.AlgoID = newAlgoID
	st.LastTrailAt = ct.End
	st.LastTrailEnd = slot

	if !st.MovedToBE && approxAtOrBeyondBE(st, s.User.Settings, newSL) {
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
