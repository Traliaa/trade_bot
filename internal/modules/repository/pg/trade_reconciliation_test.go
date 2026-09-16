package pg

import (
	"encoding/json"
	"math"
	"testing"
	"time"
	"trade_bot/internal/models"

	"github.com/google/uuid"
)

func TestReconciledStatsExcludeMissingAndMismatchedTrades(t *testing.T) {
	at := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	var snapshots []tradeReportSnapshot
	for i, pnl := range []float64{4.8, -2.2, 100, 200} {
		id := uuid.New()
		tr := models.TradeRecord{GUID: id, InstID: "ETH-USDT-SWAP", Status: models.TradeStatusClosed, EntryAt: at,
			Payload: models.TradePayload{PosSide: "long", EntrySize: 1, ExitSize: 1, TotalFees: -.2, GrossRealizedPnL: pnl + .2, RealizedPnL: pnl}}
		fills := []models.TradeFillRecord{
			{TradeGUID: id, TradeID: "in", InstID: tr.InstID, PosSide: "long", Side: "buy", Role: models.TradeFillRoleEntry, FillPrice: 100, FillSize: 1, Fee: -.1, FilledAt: at},
			{TradeGUID: id, TradeID: "out", InstID: tr.InstID, PosSide: "long", Side: "sell", Role: models.TradeFillRoleExit, FillPrice: 105, FillSize: 1, Fee: -.1, RealizedPnL: pnl + .2, FilledAt: at.Add(time.Hour)},
		}
		if i == 2 {
			fills = nil
		}
		if i == 3 {
			fills[1].RealizedPnL = 0
		}
		snapshots = append(snapshots, tradeReportSnapshot{Trade: tr, Fills: fills})
	}
	snapshots = append(snapshots, tradeReportSnapshot{Trade: models.TradeRecord{Status: models.TradeStatusOpen, Payload: models.TradePayload{UnrealizedPnL: 1.5}}})
	stats := buildReconciledTradeStats(snapshots, at)
	r := stats.Reconciliation
	if r == nil || r.Verified != 2 || r.Incomplete != 1 || r.Mismatched != 1 || len(r.Trades) != 4 {
		t.Fatalf("incorrect coverage: %+v", r)
	}
	if r.Scope != "stored_fills" || r.FundingStatus != "not_checked" || !r.CheckedAt.Equal(at) {
		t.Fatalf("scope or freshness missing: %+v", r)
	}
	v := r.VerifiedStats
	if v.Trades != 2 || v.Wins != 1 || v.Losses != 1 || v.WinRate != 50 || math.Abs(v.NetPnL-2.6) > 1e-12 || math.Abs(v.Fees+.4) > 1e-12 || v.ProfitFactor == nil || math.Abs(*v.ProfitFactor-4.8/2.2) > 1e-12 {
		t.Fatalf("unverified records polluted verified stats: %+v", v)
	}
	if stats.OpenTrades != 1 || stats.ClosedTrades != 4 || stats.TotalTrades != 5 || stats.OpenPnL != 1.5 || math.Abs(stats.TotalPnL-302.6) > 1e-12 {
		t.Fatalf("legacy totals changed: %+v", stats)
	}
}

func TestEmptyReconciledStatsDoesNotClaimProfitFactor(t *testing.T) {
	stats := buildReconciledTradeStats(nil, time.Now())
	if stats.Reconciliation == nil || stats.Reconciliation.Trades == nil || stats.Reconciliation.VerifiedStats.ProfitFactor != nil {
		t.Fatalf("empty evidence presented as known: %+v", stats)
	}
}

func TestReconciledStatsOverflowIsExplicitlyUnavailable(t *testing.T) {
	at := time.Now().UTC()
	var snapshots []tradeReportSnapshot
	for i := 0; i < 2; i++ {
		id := uuid.New()
		tr := models.TradeRecord{GUID: id, InstID: "ETH-USDT-SWAP", Status: models.TradeStatusClosed,
			Payload: models.TradePayload{PosSide: "long", EntrySize: 1, ExitSize: 1, TotalFees: -1e308, GrossRealizedPnL: 1e308}}
		fills := []models.TradeFillRecord{
			{TradeGUID: id, TradeID: "in", InstID: tr.InstID, PosSide: "long", Side: "buy", Role: models.TradeFillRoleEntry, FillPrice: 100, FillSize: 1, Fee: -1e308, FilledAt: at},
			{TradeGUID: id, TradeID: "out", InstID: tr.InstID, PosSide: "long", Side: "sell", Role: models.TradeFillRoleExit, FillPrice: 100, FillSize: 1, RealizedPnL: 1e308, FilledAt: at},
		}
		snapshots = append(snapshots, tradeReportSnapshot{Trade: tr, Fills: fills})
	}
	stats := buildReconciledTradeStats(snapshots, at)
	raw, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("overflow makes response unencodable: %v", err)
	}
	var response struct {
		Reconciliation struct {
			VerifiedStats *json.RawMessage `json:"verified_stats"`
			Status        string           `json:"verified_stats_status"`
		} `json:"reconciliation"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	if response.Reconciliation.VerifiedStats != nil || response.Reconciliation.Status != "numeric_overflow" {
		t.Fatalf("overflow presented as valid totals: %s", raw)
	}
}
