package models

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// TradeReconciliation checks internal consistency of stored fills only. It does
// not certify exchange history completeness, fee currency, or funding coverage.
type TradeReconciliation struct {
	TradeGUID  uuid.UUID  `json:"trade_guid"`
	InstID     string     `json:"inst_id"`
	Status     string     `json:"status"`
	Issues     []string   `json:"issues"`
	EntrySize  float64    `json:"entry_size"`
	ExitSize   float64    `json:"exit_size"`
	Fees       float64    `json:"fees"`
	GrossPnL   float64    `json:"gross_pnl"`
	NetPnL     float64    `json:"net_pnl"`
	LastFillAt *time.Time `json:"last_fill_at,omitempty"`
}

func ReconcileTrade(tr TradeRecord, fills []TradeFillRecord) TradeReconciliation {
	r := TradeReconciliation{TradeGUID: tr.GUID, InstID: tr.InstID, Status: "verified", Issues: []string{}}
	issue := func(status, why string) {
		r.Issues = append(r.Issues, why)
		// An early invalid-fill return may follow already-overflowed partial sums.
		for _, value := range []*float64{&r.EntrySize, &r.ExitSize, &r.Fees, &r.GrossPnL, &r.NetPnL} {
			if !finiteReconciliation(*value) {
				*value = 0
			}
		}
		if r.Status != "mismatch" {
			r.Status = status
		}
	}
	if tr.Status != TradeStatusClosed {
		issue("incomplete", "trade_not_closed")
		return r
	}
	p := tr.Payload
	if tr.GUID == uuid.Nil || tr.InstID == "" || (p.PosSide != "long" && p.PosSide != "short") ||
		!finitePositive(p.EntrySize) || !finitePositive(p.ExitSize) ||
		!finiteReconciliation(p.TotalFees) || !finiteReconciliation(p.GrossRealizedPnL) || !finiteReconciliation(p.RealizedPnL) {
		issue("mismatch", "invalid_history")
		return r
	}
	if len(fills) == 0 {
		issue("incomplete", "missing_fills")
		return r
	}
	seen := make(map[string]bool, len(fills))
	entries, exits := 0, 0
	for _, f := range fills {
		if f.TradeID == "" || !finitePositive(f.FillPrice) || !finitePositive(f.FillSize) ||
			!finiteReconciliation(f.Fee) || !finiteReconciliation(f.RealizedPnL) || f.FilledAt.IsZero() ||
			(f.Role != TradeFillRoleEntry && f.Role != TradeFillRoleExit) {
			issue("mismatch", "invalid_fill")
			return r
		}
		entrySide, exitSide := "buy", "sell"
		if p.PosSide == "short" {
			entrySide, exitSide = "sell", "buy"
		}
		if f.TradeGUID != tr.GUID || f.InstID != tr.InstID || f.PosSide != p.PosSide ||
			(f.Role == TradeFillRoleEntry && f.Side != entrySide) || (f.Role == TradeFillRoleExit && f.Side != exitSide) {
			issue("mismatch", "fill_identity_mismatch")
			return r
		}
		if seen[f.TradeID] {
			issue("mismatch", "duplicate_fill")
			return r
		}
		seen[f.TradeID] = true
		if f.Role == TradeFillRoleEntry {
			r.EntrySize += f.FillSize
			entries++
		} else {
			r.ExitSize += f.FillSize
			exits++
		}
		r.Fees += f.Fee
		r.GrossPnL += f.RealizedPnL
		if r.LastFillAt == nil || f.FilledAt.After(*r.LastFillAt) {
			at := f.FilledAt
			r.LastFillAt = &at
		}
	}
	r.NetPnL = r.GrossPnL + r.Fees
	if !finiteReconciliation(r.EntrySize) || !finiteReconciliation(r.ExitSize) || !finiteReconciliation(r.Fees) || !finiteReconciliation(r.GrossPnL) || !finiteReconciliation(r.NetPnL) {
		r.EntrySize, r.ExitSize, r.Fees, r.GrossPnL, r.NetPnL = 0, 0, 0, 0, 0
		issue("mismatch", "numeric_overflow")
		return r
	}
	if entries == 0 {
		issue("incomplete", "missing_entry_fills")
	}
	if exits == 0 {
		issue("incomplete", "missing_exit_fills")
	}
	for _, v := range []struct {
		name             string
		actual, expected float64
	}{
		{"entry", r.EntrySize, p.EntrySize}, {"exit", r.ExitSize, p.ExitSize},
	} {
		if !reconciliationNear(v.actual, v.expected, 1e-9) {
			if v.actual > v.expected {
				issue("mismatch", v.name+"_size_excess")
			} else {
				issue("incomplete", v.name+"_size_shortfall")
			}
		}
	}
	if !reconciliationNear(p.EntrySize, p.ExitSize, 1e-9) {
		issue("mismatch", "history_size_mismatch")
	}
	// Missing fills cannot establish an accounting mismatch from partial sums.
	if r.Status == "verified" {
		if !reconciliationNear(r.Fees, p.TotalFees, 1e-5) {
			issue("mismatch", "fee_mismatch")
		}
		if !reconciliationNear(r.GrossPnL, p.GrossRealizedPnL, 1e-5) {
			issue("mismatch", "gross_pnl_mismatch")
		}
		if !reconciliationNear(r.NetPnL, p.RealizedPnL, 1e-5) {
			issue("mismatch", "net_pnl_mismatch")
		}
	}
	return r
}

func finiteReconciliation(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func reconciliationNear(a, b, absolute float64) bool {
	return math.Abs(a-b) <= absolute+1e-9*math.Max(math.Abs(a), math.Abs(b))
}
