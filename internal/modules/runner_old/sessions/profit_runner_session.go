package sessions

import (
	"context"
	"fmt"
	"go.uber.org/zap"
	"time"
	"trade_bot/internal/models"
)

// Called with TrailExecMu held; exchange operations use a detached snapshot.
func (s *UserSession) trailProfitRunner(ctx context.Context, key models.PosKey, end time.Time) {
	s.TrailMu.RLock()
	original := s.TrailStates[key]
	if original == nil {
		s.TrailMu.RUnlock()
		return
	}
	st := *original
	s.TrailMu.RUnlock()
	if !st.LastTrailAt.IsZero() && end.Sub(st.LastTrailAt) < time.Minute {
		return
	}
	if s.Repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	tr, err := s.Repo.FindOpenTrade(ctx, s.User.TelegramID, st.InstID)
	if err != nil || tr == nil || tr.Payload.PosSide != st.PosSide {
		return
	}
	// Capture BEFORE the fresh position read. A manual status may become
	// terminal while protection HTTP calls are still running; this cycle must
	// not close another partial based on its earlier position snapshot.
	manualPending, manualErr := s.Repo.HasActiveManualClose(ctx, s.User.TelegramID, tr.GUID)
	// Never let an old runtime average entry redefine the initial R threshold.
	st.Entry = tr.Payload.EntryPrice
	st.RiskDist = tr.Payload.InitialRiskDist()
	st.TookPartial = st.TookPartial || tr.Payload.TookPartial || tr.Payload.PartialCount > 0 || (tr.Payload.CurrentSize > 0 && tr.Payload.CurrentSize < tr.Payload.EntrySize)
	st.RunnerPartialPending = st.RunnerPartialPending || tr.Payload.RunnerPartialPending
	if tr.Payload.RunnerPartialSize > 0 {
		st.RunnerPartialSize = tr.Payload.RunnerPartialSize
	}
	st.LastTrailAt = end // throttle failures as well as successful replacements
	oldSL := st.SL
	persist := func() error {
		s.TrailMu.Lock()
		if s.TrailStates[key] != original {
			s.TrailMu.Unlock()
			return fmt.Errorf("position state changed during runner update")
		}
		*original = st
		s.TrailMu.Unlock()
		return s.syncTradeFlagsFromState(ctx, &st, st.Size)
	}
	warn := func(err error) {
		s.Logger.Warn("profit runner update failed", zap.String("instId", st.InstID), zap.Error(err))
		if s.canSend("runner-error:"+st.InstID+":"+st.PosSide, 15*time.Minute) {
			s.Notifier.SendF(ctx, s.User.TelegramID, "⚠️ [%s %s] Не удалось подтвердить обновление сопровождения 1R: %v. Проверь защитные ордера на OKX.", st.InstID, st.PosSide, err)
		}
	}
	meta, err := reconcileRunnerProtection(ctx, s.Okx, &st, persist)
	persistErr := persist()
	if persistErr != nil {
		warn(persistErr)
		return
	}
	if err != nil {
		warn(err)
		return
	}
	if st.SL != oldSL && s.canSend("trail:"+st.InstID+":"+st.PosSide, 15*time.Minute) {
		s.Notifier.SendF(ctx, s.User.TelegramID, "🛡 [%s] Сопровождение 1R (%s): SL → %.6f. Остаток ведётся по стопу, фиксированный TP снят.", st.InstID, st.PosSide, st.SL)
	}
	if manualErr != nil {
		warn(fmt.Errorf("manual close status unknown; automatic partial blocked: %w", manualErr))
		return
	}
	if manualPending {
		return
	}
	decision := decideTrail15m(&st, s.SettingsSnapshot(), meta.LastPx, end)
	size := normalizedPartialSize(st.Size, decision.CloseSize, meta)
	if size <= 0 {
		return
	}
	// Persist intent BEFORE sending the order. An ambiguous timeout or restart
	// cannot submit a second partial; reconcile the actual smaller size instead.
	st.RunnerPartialPending = true
	st.RunnerPartialSize = st.Size
	if err := persist(); err != nil {
		warn(err)
		return
	}
	orderID, err := s.Okx.CloseMarket(ctx, st.InstID, st.PosSide, size)
	if err != nil {
		warn(fmt.Errorf("partial result unknown/rejected; automatic retry blocked: %w", err))
		return
	}
	fills, fillErr := s.Okx.WaitOrderFills(ctx, st.InstID, orderID, size, 3*time.Second)
	if fillErr == nil {
		if err := s.Repo.UpsertTradeFills(ctx, tradeFillRecordsForSession(*tr, fills, models.TradeFillRoleExit)); err != nil {
			warn(err)
		}
	}
	// Fresh size decides whether it filled; never subtract the requested size.
	_, err = reconcileRunnerProtection(ctx, s.Okx, &st, persist)
	if persistErr := persist(); persistErr != nil {
		warn(persistErr)
		return
	}
	if err != nil {
		warn(err)
		return
	}
	if st.RunnerPartialPending {
		warn(fmt.Errorf("partial execution not confirmed; retry blocked"))
		return
	}
	if st.TookPartial {
		s.Notifier.SendF(ctx, s.User.TelegramID, "💰 [%s] Частичная фиксация подтверждена. Остаток %.6f сопровождается SL %.6f (1R).", st.InstID, st.Size, st.SL)
	}
}
