package sessions

import (
	"math"
	"testing"
	"time"
	"trade_bot/internal/models"
)

func TestPartialDecisionNormalizesOrKeepsStopProtection(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		size, lot, min, want float64
	}{{"SKHYNIX", .007, .001, .001, .003}, {"MRVL", .03, .01, .01, .01}, {"minimum position", .01, .01, .01, 0}, {"unknown metadata", .03, 0, 0, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			st := &models.PositionTrailState{Entry: 100, SL: 90, RiskDist: 10, Size: tc.size, PosSide: "long", MFE: 115}
			cfg := models.Settings{TrailingConfig: models.TrailingConfig{PartialEnabled: true, PartialTriggerR: 1.25, PartialCloseFrac: .5, BETriggerR: 1, BEOffsetR: .1}}
			now := time.Now()
			dec := decideTrail15m(st, cfg, 115, now)
			got := normalizePartialDecision(st, cfg, 115, now, dec, models.Instrument{LotSz: tc.lot, MinSz: tc.min})
			if math.Abs(got.CloseSize-tc.want) > 1e-12 {
				t.Fatalf("close=%v want %v", got.CloseSize, tc.want)
			}
			if tc.want == 0 && (!got.MoveSL || got.NewSL != 101) {
				t.Fatalf("partial failure blocks BE: %+v", got)
			}
			if st.TookPartial {
				t.Fatal("unexecuted partial marked as filled")
			}
		})
	}
}

func TestTrailRecoveryKeepsLatestProtectionAndInitialRisk(t *testing.T) {
	p := models.TradePayload{PosSide: "long", EntryPrice: 100, EntrySize: 2, CurrentSize: 1, StopLoss: 90, RiskDist: 10, TakeProfit: 120}
	st := &models.PositionTrailState{PosSide: "long", SL: 101, AlgoID: "new-sl", TPAlgoID: "new-tp", MovedToBE: true, TookPartial: true}
	applyTrailReportState(&p, st, 1)
	restored := trailStateFromTrade(models.TradeRecord{InstID: "BTC-USDT-SWAP", Payload: p})
	if restored.SL != 101 || restored.AlgoID != "new-sl" || restored.TPAlgoID != "new-tp" || restored.Size != 1 {
		t.Fatalf("stale protection restored: %+v", restored)
	}
	if p.StopLoss != 90 || p.RiskDist != 10 {
		t.Fatal("initial risk overwritten")
	}
}

func TestExistingExchangePositionBlocksNewEntryRegardlessOfSide(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		positions := []models.OpenPosition{{Symbol: "AVAX-USDT-SWAP", Side: side, Size: 1}}
		if err := rejectExistingExchangePosition(positions, "AVAX-USDT-SWAP"); err == nil {
			t.Fatal("existing position ignored")
		}
		if err := rejectExistingExchangePosition(positions, "BTC-USDT-SWAP"); err != nil {
			t.Fatal(err)
		}
	}
}
