package models

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func reconciliationFixture() (TradeRecord, []TradeFillRecord) {
	at := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	end := at.Add(time.Hour)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	tr := TradeRecord{GUID: id, InstID: "ETH-USDT-SWAP", Status: TradeStatusClosed, EntryAt: at, ExitAt: &end,
		Payload: TradePayload{PosSide: "long", EntrySize: 2, ExitSize: 2, TotalFees: -.4, GrossRealizedPnL: 6, RealizedPnL: 5.6}}
	fills := []TradeFillRecord{
		{TradeGUID: id, TradeID: "entry", InstID: tr.InstID, PosSide: "long", Side: "buy", Role: TradeFillRoleEntry, FillPrice: 100, FillSize: 2, Fee: -.2, FilledAt: at},
		{TradeGUID: id, TradeID: "partial", InstID: tr.InstID, PosSide: "long", Side: "sell", Role: TradeFillRoleExit, FillPrice: 102, FillSize: 1, Fee: -.1, RealizedPnL: 2, FilledAt: at.Add(time.Minute)},
		{TradeGUID: id, TradeID: "exit", InstID: tr.InstID, PosSide: "long", Side: "sell", Role: TradeFillRoleExit, FillPrice: 104, FillSize: 1, Fee: -.1, RealizedPnL: 4, FilledAt: end},
	}
	return tr, fills
}

func TestReconciliationPartialCloseAndRebate(t *testing.T) {
	tr, fills := reconciliationFixture()
	got := ReconcileTrade(tr, fills)
	if got.Status != "verified" || len(got.Issues) != 0 || got.EntrySize != 2 || got.ExitSize != 2 || math.Abs(got.NetPnL-5.6) > 1e-12 {
		t.Fatalf("partial fills not reconciled: %+v", got)
	}
	// A positive fee is a rebate, not a cost to subtract again.
	fills[0].Fee = .2
	tr.Payload.TotalFees = 0
	tr.Payload.RealizedPnL = 6
	if got := ReconcileTrade(tr, fills); got.Status != "verified" || got.NetPnL != 6 {
		t.Fatalf("rebate incorrectly accounted: %+v", got)
	}
}

func TestReconciliationRejectsIncompleteAndInconsistentEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, status, issue string
		change              func(*TradeRecord, *[]TradeFillRecord)
	}{
		{"no fills", "incomplete", "missing_fills", func(_ *TradeRecord, f *[]TradeFillRecord) { *f = nil }},
		{"only exits", "incomplete", "missing_entry_fills", func(_ *TradeRecord, f *[]TradeFillRecord) { *f = (*f)[1:] }},
		{"only entry", "incomplete", "missing_exit_fills", func(_ *TradeRecord, f *[]TradeFillRecord) { *f = (*f)[:1] }},
		{"partial remainder missing", "incomplete", "exit_size_shortfall", func(_ *TradeRecord, f *[]TradeFillRecord) { *f = (*f)[:2] }},
		{"oversized exit", "mismatch", "exit_size_excess", func(_ *TradeRecord, f *[]TradeFillRecord) { (*f)[2].FillSize = 2 }},
		{"duplicate", "mismatch", "duplicate_fill", func(_ *TradeRecord, f *[]TradeFillRecord) { *f = append(*f, (*f)[2]) }},
		{"foreign trade", "mismatch", "fill_identity_mismatch", func(_ *TradeRecord, f *[]TradeFillRecord) { (*f)[2].TradeGUID = uuid.New() }},
		{"wrong side", "mismatch", "fill_identity_mismatch", func(_ *TradeRecord, f *[]TradeFillRecord) { (*f)[2].Side = "buy" }},
		{"fees", "mismatch", "fee_mismatch", func(tr *TradeRecord, _ *[]TradeFillRecord) { tr.Payload.TotalFees = 0 }},
		{"net", "mismatch", "net_pnl_mismatch", func(tr *TradeRecord, _ *[]TradeFillRecord) { tr.Payload.RealizedPnL = 6 }},
		{"gross", "mismatch", "gross_pnl_mismatch", func(tr *TradeRecord, _ *[]TradeFillRecord) { tr.Payload.GrossRealizedPnL = 5 }},
		{"not closed", "incomplete", "trade_not_closed", func(tr *TradeRecord, _ *[]TradeFillRecord) { tr.Status = TradeStatusOpen }},
		{"invalid number", "mismatch", "invalid_fill", func(_ *TradeRecord, f *[]TradeFillRecord) { (*f)[2].Fee = math.NaN() }},
		{"invalid payload", "mismatch", "invalid_history", func(tr *TradeRecord, _ *[]TradeFillRecord) { tr.Payload.ExitSize = math.Inf(1) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, fills := reconciliationFixture()
			tc.change(&tr, &fills)
			got := ReconcileTrade(tr, fills)
			if got.Status != tc.status || !slices.Contains(got.Issues, tc.issue) {
				t.Fatalf("got %+v; want %s with %s", got, tc.status, tc.issue)
			}
		})
	}
}

func TestReconciliationShortAndFloatingPointTolerance(t *testing.T) {
	tr, fills := reconciliationFixture()
	tr.Payload.PosSide = "short"
	for i := range fills {
		fills[i].PosSide = "short"
		if fills[i].Role == TradeFillRoleEntry {
			fills[i].Side = "sell"
		} else {
			fills[i].Side = "buy"
		}
	}
	tr.Payload.RealizedPnL += 1e-7
	tr.Payload.ExitSize += 1e-12
	if got := ReconcileTrade(tr, fills); got.Status != "verified" {
		t.Fatalf("rounding treated as mismatch: %+v", got)
	}
}

func TestReconciliationOverflowBeforeInvalidFillIsEncodable(t *testing.T) {
	tr, fills := reconciliationFixture()
	fills[0].Fee = 1e308
	fills[1].Fee = 1e308
	fills[2].Fee = math.NaN()
	r := ReconcileTrade(tr, fills)
	if r.Status != "mismatch" {
		t.Fatalf("overflow accepted: %+v", r)
	}
	if _, err := json.Marshal(r); err != nil {
		t.Fatalf("invalid sums leaked into response: %v", err)
	}
}
