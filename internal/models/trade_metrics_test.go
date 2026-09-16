package models

import (
	"math"
	"testing"
)

func TestPriceRUsesImmutableRisk(t *testing.T) {
	for _, tc := range []struct {
		side        string
		price, want float64
	}{{"long", 105, .5}, {"short", 95, .5}, {"long", 97, -.3}, {"short", 103, -.3}} {
		p := TradePayload{EntryPrice: 100, StopLoss: 101, RiskDist: 10, PosSide: tc.side, MovedToBE: true}
		if got := CalcPriceR(100, tc.price, p.InitialRiskDist(), tc.side); math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("%+v: got %v", tc, got)
		}
	}
}

func TestInitialRiskFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    TradePayload
		want float64
	}{
		{"saved", TradePayload{RiskDist: 10, EntryPrice: 100, StopLoss: 99, PosSide: "long"}, 10},
		{"planned", TradePayload{EntryPrice: 100, StopLoss: 101, PosSide: "long", MovedToBE: true, PlannedRiskUSDT: 20, EntrySize: 4, CtVal: .5}, 10},
		{"legacy", TradePayload{EntryPrice: 100, StopLoss: 90, PosSide: "long"}, 10},
		{"legacy short", TradePayload{EntryPrice: 100, StopLoss: 110, PosSide: "short"}, 10},
		{"moved with missing risk", TradePayload{EntryPrice: 100, StopLoss: 99, PosSide: "long", MovedToBE: true}, 0},
		{"locked with missing risk", TradePayload{EntryPrice: 100, StopLoss: 99, PosSide: "long", LockedProfit: true}, 0},
		{"invalid saved risk", TradePayload{EntryPrice: 100, StopLoss: 90, PosSide: "long", RiskDist: math.NaN()}, 0},
		{"planned replaces negative saved risk", TradePayload{RiskDist: -1, PlannedRiskUSDT: 20, EntrySize: 4, CtVal: .5}, 10},
		{"planned replaces nonfinite saved risk", TradePayload{RiskDist: math.NaN(), PlannedRiskUSDT: 20, EntrySize: 4, CtVal: .5}, 10},
		{"incomplete planned risk", TradePayload{EntryPrice: 100, StopLoss: 90, PosSide: "long", PlannedRiskUSDT: 20, EntrySize: 4}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.p.InitialRiskDist(); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestPriceRRejectsInvalidAndOverflow(t *testing.T) {
	for _, v := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, args := range [][3]float64{{v, 105, 10}, {100, v, 10}, {100, 105, v}} {
			if got := CalcPriceR(args[0], args[1], args[2], "long"); got != 0 {
				t.Fatalf("invalid inputs %v produced %v", args, got)
			}
		}
	}
	if got := CalcPriceR(1, math.MaxFloat64, math.SmallestNonzeroFloat64, "long"); got != 0 {
		t.Fatalf("overflow produced %v", got)
	}
	if got := CalcPriceR(100, 105, 10, "unknown"); got != 0 {
		t.Fatal(got)
	}
}
