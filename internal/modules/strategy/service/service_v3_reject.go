package service

import (
	"slices"
	"trade_bot/internal/models"
)

// v3EntryRejectReason mirrors the entry gates in onCandleV3ReadyLocked.
// Score.Reasons also includes diagnostics that are not standalone entry gates
// (such as volatility), so its first item is not necessarily why entry failed.
// A disabled short is reported only when it would otherwise qualify for entry.
func v3EntryRejectReason(score models.SignalScore, oppositeScore, minConfirm, minEdge int, side models.Side, allowShorts bool) models.RejectReason {
	if !score.ContextOK {
		for _, reason := range score.Reasons {
			switch reason {
			case models.RejectHTFConflict, models.RejectCompressedRange,
				models.RejectOverextendedUp, models.RejectOverextendedDown, models.RejectLowVolume:
				return reason
			}
		}
		// Do not invent a low score when context diagnostics are incomplete.
		return models.RejectInternal
	}
	if !score.SetupOK {
		return models.RejectRetestNotConfirmed
	}
	if !score.StrongClose {
		if side == models.SideSell {
			return models.RejectWeakCloseDown
		}
		return models.RejectWeakCloseUp
	}
	if !score.ImpulseOK {
		if slices.Contains(score.Reasons, models.RejectImpulseTooStrong) {
			return models.RejectImpulseTooStrong
		}
		return models.RejectImpulseWeak
	}
	if !score.StructureOK {
		return models.RejectStructureNotConfirmed
	}
	if score.Score < minConfirm {
		return models.RejectConfirmScoreLow
	}
	if score.Score < oppositeScore+minEdge {
		return models.RejectScoreEdgeLow
	}
	if side == models.SideSell && !allowShorts {
		return models.RejectShortsDisabled
	}
	return ""
}
