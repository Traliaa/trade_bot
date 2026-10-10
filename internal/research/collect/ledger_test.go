package collect

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func ledgerFixture() LedgerInput {
	at := time.Date(2026, 10, 5, 7, 15, 3, 0, time.UTC)
	fills := []Fill{{ID: "PRIVATE-entry", OrderID: "PRIVATE-order-entry", Symbol: "ARB-USDT-SWAP", Side: "buy", PosSide: "long", At: at, Size: "1.8", Price: "0.20648", Fee: "-0.00185832", PnL: "0", FeeCurrency: "USDT"}}
	for i, f := range []struct{ size, pnl, fee string }{{"0.4", "-0.00788", "-0.00040902"}, {"0.9", "-0.01773", "-0.000920295"}, {"0.4", "-0.00788", "-0.00040902"}, {"0.1", "-0.00197", "-0.000102255"}} {
		fills = append(fills, Fill{ID: []string{"PRIVATE-a", "PRIVATE-b", "PRIVATE-c", "PRIVATE-d"}[i], OrderID: "PRIVATE-order-exit", Symbol: "ARB-USDT-SWAP", Side: "sell", PosSide: "long", At: at.Add(time.Hour), Size: f.size, Price: "0.20451", Fee: f.fee, PnL: f.pnl, FeeCurrency: "USDT"})
	}
	return LedgerInput{Symbol: "ARB-USDT-SWAP", Side: "long", EntryAt: at, Cutoff: at.Add(2 * time.Hour), EntrySize: "1.8", EntryOrderIDs: []string{"PRIVATE-order-entry"}, ExitOrderIDs: []string{"PRIVATE-order-exit"}, Fills: fills, ArchiveComplete: true, DBState: "closed"}
}

func TestLedgerExactARB(t *testing.T) {
	in := ledgerFixture()
	in.Fills = append(in.Fills, in.Fills[2])
	r := Reconcile(in)
	if r.Reconciliation != "matched" || r.EntrySize != "1.8" || r.ExitSize != "1.8" || r.Gross != "-0.03546" || r.Fees != "-0.00369891" || r.TradingNet == nil || *r.TradingNet != "-0.03915891" || r.Net != nil || r.Costs != "sensitivity-only" || r.FillCount != 5 {
		t.Fatalf("report: %+v", r)
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(string(raw), "PRIVATE") {
		t.Fatal("private IDs leaked")
	}
	if _, err := json.Marshal(in); err == nil {
		t.Fatal("private input can be accidentally serialized")
	}
	if _, err := json.Marshal(in.Fills[0]); err == nil {
		t.Fatal("private fill can be accidentally serialized")
	}
	slices.Reverse(in.Fills)
	if !reflect.DeepEqual(r, Reconcile(in)) {
		t.Fatal("input order changed report")
	}
}

func TestLedgerRejectsBadEvidence(t *testing.T) {
	cases := map[string]func(*LedgerInput){
		"conflicting_duplicate":      func(i *LedgerInput) { f := i.Fills[1]; f.OrderID = "other"; i.Fills = append(i.Fills, f) },
		"conflicting_duplicate_size": func(i *LedgerInput) { f := i.Fills[1]; f.Size = "0.3"; i.Fills = append(i.Fills, f) },
		"exit_before_first_fill":     func(i *LedgerInput) { i.Fills[0].At = i.EntryAt.Add(90 * time.Minute) },
		"foreign_order":              func(i *LedgerInput) { i.Fills[1].OrderID = "other" },
		"foreign_symbol":             func(i *LedgerInput) { i.Fills[1].Symbol = "ETH-USDT-SWAP" },
		"wrong_side":                 func(i *LedgerInput) { i.Fills[1].Side = "buy" },
		"wrong_pos_side":             func(i *LedgerInput) { i.Fills[1].PosSide = "short" },
		"invalid_decimal":            func(i *LedgerInput) { i.Fills[1].Size = "1e10000000" },
		"over_exit":                  func(i *LedgerInput) { i.Fills[1].Size = "0.40000000000000001" },
		"before_entry":               func(i *LedgerInput) { i.Fills[1].At = i.EntryAt.Add(-time.Millisecond) },
		"after_cutoff":               func(i *LedgerInput) { i.Fills[1].At = i.Cutoff.Add(time.Millisecond) },
		"missing_order":              func(i *LedgerInput) { i.ExitOrderIDs = nil },
		"overlapping_orders":         func(i *LedgerInput) { i.ExitOrderIDs = append(i.ExitOrderIDs, i.EntryOrderIDs[0]) },
		"unknown_increase":           func(i *LedgerInput) { i.EntryOrderIDs = append(i.EntryOrderIDs, "extra") },
		"recorded_net":               func(i *LedgerInput) { s := "100"; i.RecordedTradingNet = &s },
		"db_open":                    func(i *LedgerInput) { i.DBState = "open"; s := "0"; i.ExchangeRemaining = &s },
		"unknown_db_state":           func(i *LedgerInput) { i.DBState = "unknown" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			i := ledgerFixture()
			mutate(&i)
			r := Reconcile(i)
			if r.Reconciliation != "mismatch" || r.Net != nil || r.TradingNet != nil {
				t.Fatalf("unsafe result: %+v", r)
			}
		})
	}
}

func TestLedgerPendingAndCurrencies(t *testing.T) {
	i := ledgerFixture()
	i.ArchiveComplete = false
	r := Reconcile(i)
	if r.Reconciliation != "pending" || r.TradingNet != nil {
		t.Fatalf("partial archive accepted: %+v", r)
	}
	i = ledgerFixture()
	i.Fills[1].FeeCurrency = "BTC"
	r = Reconcile(i)
	if r.Costs != "unknown" || r.TradingNet != nil || r.Net != nil || r.Fees != "" {
		t.Fatalf("mixed-currency net: %+v", r)
	}
	i = ledgerFixture()
	i.Fills[0].Fee = "0.00185832" // rebate
	r = Reconcile(i)
	if r.TradingNet == nil || *r.TradingNet != "-0.03544227" {
		t.Fatalf("rebate lost: %+v", r)
	}
	i = ledgerFixture()
	i.Funding = &FundingEvidence{Amount: "0", Currency: "USDT", Complete: true}
	r = Reconcile(i)
	if r.Net == nil || *r.Net != "-0.03915891" || r.Costs != "verified" {
		t.Fatalf("verified zero lost: %+v", r)
	}
	i.Funding.Complete = false
	r = Reconcile(i)
	if r.Net != nil || r.Costs == "verified" {
		t.Fatal("unknown funding became zero")
	}
}

func TestLedgerAVAXAndPartial(t *testing.T) {
	i := ledgerFixture()
	i.EntrySize = "1"
	i.Fills[0].Size = "1"
	i.Fills = i.Fills[:3]
	i.Fills[1].Size = "0.5"
	i.Fills[2].Size = "1"
	if r := Reconcile(i); r.Reconciliation != "mismatch" {
		t.Fatal("AVAX extra volume accepted")
	}
	i = ledgerFixture()
	i.Fills = i.Fills[:2]
	i.DBState = "open"
	remaining := "1.4"
	i.ExchangeRemaining = &remaining
	r := Reconcile(i)
	if r.Reconciliation != "matched" || r.Closed || r.Net != nil || r.TradingNet != nil {
		t.Fatalf("open partial: %+v", r)
	}
	remaining = "0"
	if r := Reconcile(i); r.Reconciliation != "mismatch" {
		t.Fatal("DB-open exchange-flat hidden")
	}
}

func TestLedgerMultiTimeEntry(t *testing.T) {
	i := ledgerFixture()
	i.Fills[0].Size = "0.9"
	f := i.Fills[0]
	f.ID = "PRIVATE-entry-2"
	f.At = f.At.Add(time.Millisecond)
	i.Fills = append(i.Fills, f)
	r := Reconcile(i)
	if !slices.Contains(r.Reasons, "multi_time_entry") || r.Reconciliation != "matched" {
		t.Fatalf("multi-time hidden: %+v", r)
	}
}
