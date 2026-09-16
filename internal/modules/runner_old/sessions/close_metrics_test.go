package sessions

import (
	"math"
	"testing"
	"time"
	"trade_bot/internal/models"
)

func TestOpenReportPreservesLiveRecoveryRisk(t *testing.T) {
	at := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	tr := models.TradeRecord{EntryAt: at, Payload: models.TradePayload{
		EntryPrice: 100, StopLoss: 99, TakeProfit: 120, PosSide: "long",
		PlannedRiskUSDT: 20, EntrySize: 4, CtVal: .5, MFEPrice: 108, MAEPrice: 97,
	}}
	before := trailStateFromTrade(tr)
	got := openTradeReportPayload(tr, models.OpenPosition{MarkPx: 105, Size: 4, CtVal: .5}, at.Add(time.Hour))
	if got.RiskDist != 0 || got.StopLoss != 99 || got.PlannedRiskUSDT != 20 {
		t.Fatalf("report changed recovery inputs: %+v", got)
	}
	tr.Payload = got
	after := trailStateFromTrade(tr)
	if before.RiskDist != 1 || after.RiskDist != before.RiskDist {
		t.Fatalf("live risk changed from %v to %v", before.RiskDist, after.RiskDist)
	}
	for name, pair := range map[string][2]float64{
		"price R": {got.RMultiple, .5}, "MFE": {got.MFER, .8}, "MAE": {got.MAER, -.3},
	} {
		if math.Abs(pair[0]-pair[1]) > 1e-12 {
			t.Fatalf("%s got %v want %v", name, pair[0], pair[1])
		}
	}
}

func TestClosedInputPreservesEvidenceAndNetVersusPriceR(t *testing.T) {
	at := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	tr := models.TradeRecord{EntryAt: at, Payload: models.TradePayload{EntryPrice: 100, EntrySize: 1, StopLoss: 101, TakeProfit: 120, PosSide: "long", CtVal: 1, RiskDist: 10, PlannedRiskUSDT: 10, MovedToBE: true, MFEPrice: 108, MAEPrice: 97, TotalFees: -.1, PendingCloseReason: "time_stop"}}
	execution := closedTradeExecution{ExitAt: at.Add(time.Hour), ExitPrice: 105, FinalFillPrice: 105, ExitSize: 1, GrossRealizedPnL: 5, TotalFees: -.2, Fills: []models.TradeFill{{FillPx: 105, FillSz: 1}}}
	got := closedTradeInput(tr, execution)
	for name, pair := range map[string][2]float64{"priceR": {got.Payload.ExitPriceR, .5}, "netR": {got.Payload.RMultiple, .47}, "effectiveR": {got.Payload.EffectiveR, .47}, "mfe": {got.Payload.MFER, .8}, "mae": {got.Payload.MAER, -.3}, "net": {got.Payload.RealizedPnL, 4.7}, "fees": {got.Payload.TotalFees, -.3}} {
		if math.Abs(pair[0]-pair[1]) > 1e-12 {
			t.Fatalf("%s got %v want %v", name, pair[0], pair[1])
		}
	}
	if got.CloseReason != models.CloseReasonTimeStop || got.Payload.CloseIntentReason != "time_stop" || got.Payload.CloseReasonSource != "bot_intent" || got.Payload.PendingCloseReason != "" {
		t.Fatalf("lost close evidence: %+v", got)
	}
	if got.Payload.DurationSec != 3600 {
		t.Fatalf("duration: %d", got.Payload.DurationSec)
	}
	raw, err := got.Payload.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := models.UnmarshalTradePayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if restored.CloseReasonSource != "bot_intent" || restored.CloseIntentReason != "time_stop" {
		t.Fatalf("lost persisted evidence: %+v", restored)
	}
	old, err := models.UnmarshalTradePayload([]byte(`{"entry_price":100,"pos_side":"long"}`))
	if err != nil || old.CloseReasonSource != "" {
		t.Fatalf("legacy payload not readable: %v %+v", err, old)
	}
}
