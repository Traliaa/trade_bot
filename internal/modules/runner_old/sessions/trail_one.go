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

		closeOrderID, err := s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, dec.CloseSize)
		if err != nil {
			s.Logger.Warn("partial close failed",
				zap.Error(err),
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
				zap.Float64("closeSize", dec.CloseSize),
			)
			return
		}

		actualCloseSize := dec.CloseSize
		partialFills, fillErr := s.Okx.WaitOrderFills(ctx, st.InstID, closeOrderID, dec.CloseSize, 3*time.Second)
		if fillErr != nil {
			s.Logger.Warn("partial fill reconciliation failed; using requested size",
				zap.Error(fillErr),
				zap.String("instId", st.InstID),
				zap.String("orderID", closeOrderID),
			)
		} else if _, fillSize := fillVWAP(partialFills); fillSize > 0 {
			actualCloseSize = fillSize
			if tr, findErr := s.Repo.FindOpenTrade(ctx, s.User.TelegramID, st.InstID); findErr != nil {
				s.Logger.Warn("find trade for partial fills failed", zap.Error(findErr), zap.String("instId", st.InstID))
			} else if tr != nil {
				if persistErr := s.Repo.UpsertTradeFills(ctx, tradeFillRecordsForSession(*tr, partialFills, models.TradeFillRoleExit)); persistErr != nil {
					s.Logger.Warn("persist partial fills failed", zap.Error(persistErr), zap.String("instId", st.InstID))
				}
			}
		}

		s.TrailMu.Lock()
		if st.Size > actualCloseSize {
			st.Size -= actualCloseSize
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
				zap.Float64("closeSize", actualCloseSize),
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

		// После partial приводим размер TP к фактическому остатку. Новый
		// reduce-only ордер ставится до отмены старого, чтобы не создавать окно
		// без защиты прибыли.
		if st.TPAlgoID != "" && st.TP > 0 && newSize > 0 {
			newTPAlgoID, err := s.replaceProtectiveAlgo(ctx, st, st.TPAlgoID, newSize, st.TP, true)
			if err != nil {
				s.Logger.Warn("resize post-partial TP failed",
					zap.Error(err),
					zap.String("instId", st.InstID),
					zap.String("posSide", st.PosSide),
					zap.Float64("size", newSize),
				)
			} else {
				s.TrailMu.Lock()
				st.TPAlgoID = newTPAlgoID
				s.TrailMu.Unlock()
			}
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

				newAlgoID, err := s.replaceProtectiveAlgo(ctx, st, st.AlgoID, newSize, newSL, false)
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
			zap.Float64("closeSize", actualCloseSize),
			zap.Bool("isStale", st.IsStale),
		)

		if s.canSend("partial:"+st.InstID+":"+st.PosSide, 30*time.Minute) {
			msg := dec.Note
			if msg == "" {
				msg = string(dec.Reason)
			}

			s.Notifier.SendF(ctx, s.User.TelegramID,
				"💰 [%s] Частичная фиксация (%s) закрыто=%.4f | %s",
				st.InstID, st.PosSide, actualCloseSize, msg,
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

	newAlgoID, err := s.replaceProtectiveAlgo(ctx, st, st.AlgoID, st.Size, newSL, false)
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

// replaceProtectiveAlgo first creates a new reduce-only protective order and
// only then cancels the old one. A failed placement therefore leaves the
// existing protection intact. A failed cancellation is logged: keeping two
// reduce-only orders briefly is safer than leaving the position unprotected.
func (s *UserSession) replaceProtectiveAlgo(
	ctx context.Context,
	st *models.PositionTrailState,
	oldAlgoID string,
	size float64,
	triggerPrice float64,
	isTP bool,
) (string, error) {
	newAlgoID, err := s.Okx.PlaceSingleAlgo(ctx, st.InstID, st.PosSide, size, triggerPrice, isTP)
	if err != nil {
		s.recordProtectiveReplace(ctx, st, triggerPrice, isTP, false, false)
		return "", err
	}

	cancelFailed := false
	if oldAlgoID != "" {
		if err := s.Okx.CancelAlgo(ctx, st.InstID, oldAlgoID); err != nil {
			cancelFailed = true
			s.Logger.Warn("old protective algo cancellation failed",
				zap.Error(err),
				zap.String("instId", st.InstID),
				zap.String("posSide", st.PosSide),
				zap.String("oldAlgoID", oldAlgoID),
				zap.String("newAlgoID", newAlgoID),
				zap.Bool("isTP", isTP),
			)
		}
	}
	s.recordProtectiveReplace(ctx, st, triggerPrice, isTP, true, cancelFailed)

	return newAlgoID, nil
}

func (s *UserSession) recordProtectiveReplace(
	ctx context.Context,
	st *models.PositionTrailState,
	triggerPrice float64,
	isTP bool,
	succeeded bool,
	cancelFailed bool,
) {
	if err := s.syncTradePayloadFromTrail(ctx, st.InstID, st.PosSide, func(p *models.TradePayload) {
		if isTP {
			p.TPReplaceAttempts++
			if !succeeded {
				p.TPReplaceFailures++
			}
		} else {
			p.SLReplaceAttempts++
			if !succeeded {
				p.SLReplaceFailures++
			}
			if approxAtOrBeyondBE(st, s.SettingsSnapshot(), triggerPrice) {
				p.BEReplaceAttempts++
				if !succeeded {
					p.BEReplaceFailures++
				}
			}
		}
		if cancelFailed {
			p.AlgoCancelFailures++
		}
	}); err != nil {
		s.Logger.Warn("persist protective replacement telemetry failed",
			zap.Error(err),
			zap.String("instId", st.InstID),
			zap.String("posSide", st.PosSide),
			zap.Bool("isTP", isTP),
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
