package pg

import (
	"testing"

	"trade_bot/internal/models"
)

func TestBuildTradeStatsIncludesOptimizationBreakdowns(t *testing.T) {
	trades := []models.TradeRecord{
		{
			CloseReason: models.CloseReasonTP,
			Payload: models.TradePayload{
				PosSide:             "short",
				EntrySignalScore:    6,
				EntryImpulseBodyPct: 0.004,
				RMultiple:           1.5,
				RealizedPnL:         15,
			},
		},
		{
			CloseReason: models.CloseReasonSL,
			Payload: models.TradePayload{
				PosSide:             "short",
				EntrySignalScore:    6,
				EntryImpulseBodyPct: 0.0045,
				RMultiple:           -1,
				RealizedPnL:         -10,
			},
		},
		{
			CloseReason: models.CloseReasonTimeStop,
			Payload: models.TradePayload{
				PosSide:             "long",
				EntrySignalScore:    5,
				EntryImpulseBodyPct: 0.007,
				RMultiple:           -0.5,
				RealizedPnL:         -5,
			},
		},
	}

	stats := buildTradeStats(trades)
	if stats.ClosedTrades != 3 || stats.TotalR != 0 || stats.TotalPnL != 0 {
		t.Fatalf("unexpected totals: %+v", stats)
	}
	if len(stats.ByDirection) != 2 || len(stats.BySignalScore) != 2 || len(stats.ByImpulse) != 2 {
		t.Fatalf("missing breakdowns: %+v", stats)
	}
	if len(stats.Windows) != 2 || stats.Windows[0].Key != "recent_50" || stats.Windows[0].Trades != 3 {
		t.Fatalf("unexpected rolling windows: %+v", stats.Windows)
	}

	score6 := findBreakdown(t, stats.BySignalScore, "6")
	if score6.Trades != 2 || score6.TotalR != 0.5 || score6.ProfitFactor != 1.5 {
		t.Fatalf("unexpected score=6 breakdown: %+v", score6)
	}
	impulseHigh := findBreakdown(t, stats.ByImpulse, ">0.60%")
	if impulseHigh.Trades != 1 || impulseHigh.TotalR != -0.5 {
		t.Fatalf("unexpected high impulse breakdown: %+v", impulseHigh)
	}
}

func TestImpulseBucketBoundaries(t *testing.T) {
	cases := map[float64]string{
		0:     "unknown",
		0.002: "<0.30%",
		0.003: "0.30-0.60%",
		0.006: "0.30-0.60%",
		0.007: ">0.60%",
	}
	for input, want := range cases {
		if got := impulseBucket(input); got != want {
			t.Fatalf("impulseBucket(%v)=%q, want %q", input, got, want)
		}
	}
}

func findBreakdown(t *testing.T, groups []models.TradeStatsBreakdown, key string) models.TradeStatsBreakdown {
	t.Helper()
	for _, group := range groups {
		if group.Key == key {
			return group
		}
	}
	t.Fatalf("breakdown %q not found in %+v", key, groups)
	return models.TradeStatsBreakdown{}
}
