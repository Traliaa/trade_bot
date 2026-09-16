package sessions

import (
	"math"
	"trade_bot/internal/models"
)

// A price match is an inference, never exchange-confirmed evidence. Use the
// persisted trade snapshot, not symbol-level state that may belong to a new trade.
func classifyCloseEvidence(p models.TradePayload, _ *models.PositionTrailState, exit float64) (models.CloseReason, string) {
	if p.PendingCloseReason != "" {
		reason := models.NormalizeCloseReason(p.PendingCloseReason)
		if reason == models.CloseReasonUnknown {
			return reason, "unknown"
		}
		return reason, "bot_intent"
	}
	if p.TimeStopTriggered {
		return models.CloseReasonTimeStop, "legacy_time_flag"
	}
	valid := func(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
	if !valid(exit) || !valid(p.EntryPrice) || (p.PosSide != "long" && p.PosSide != "short") {
		return models.CloseReasonUnknown, "unknown"
	}
	risk := p.InitialRiskDist()
	if !valid(risk) {
		return models.CloseReasonUnknown, "unknown"
	}
	near := func(level float64) bool { return valid(level) && approxLevel(exit, level, risk*0.4) }
	profitable := func(price float64) bool {
		return p.PosSide == "long" && price > p.EntryPrice || p.PosSide == "short" && price < p.EntryPrice
	}
	tp, sl := near(p.TakeProfit), near(p.StopLoss)
	protectiveReason := models.CloseReasonUnknown
	// Prefer the current protective level when it has been explicitly moved.
	lockLevel := p.BEPrice
	if !valid(lockLevel) {
		lockLevel = p.StopLoss
	}
	if p.LockedProfit && profitable(lockLevel) && profitable(exit) && near(lockLevel) {
		protectiveReason = models.CloseReasonLockProfit
	}
	if p.MovedToBE && !p.LockedProfit {
		be := p.BEPrice
		if !valid(be) {
			be = p.EntryPrice
		}
		if near(be) {
			protectiveReason = models.CloseReasonBreakEven
		}
	}
	if tp && (sl || protectiveReason != models.CloseReasonUnknown) {
		return models.CloseReasonUnknown, "unknown"
	}
	if protectiveReason != models.CloseReasonUnknown {
		return protectiveReason, "price_level_inferred"
	}
	if tp {
		return models.CloseReasonTP, "price_level_inferred"
	}
	if sl {
		return models.CloseReasonSL, "price_level_inferred"
	}
	return models.CloseReasonUnknown, "unknown"
}
