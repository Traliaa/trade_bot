package sessions

import (
	"math"
	"testing"
	"trade_bot/internal/models"
)

func TestCloseEvidenceSourceAndPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.TradePayload)
		exit   float64
		reason models.CloseReason
		source string
	}{
		{"manual intent near TP", func(p *models.TradePayload) { p.PendingCloseReason = "manual" }, 120, models.CloseReasonManual, "bot_intent"},
		{"time intent near SL", func(p *models.TradePayload) { p.PendingCloseReason = "time_stop_early" }, 90, models.CloseReasonTimeStopEarly, "bot_intent"},
		{"invalid intent", func(p *models.TradePayload) { p.PendingCloseReason = "invalid" }, 120, models.CloseReasonUnknown, "unknown"},
		{"time flag near TP", func(p *models.TradePayload) { p.TimeStopTriggered = true }, 120, models.CloseReasonTimeStop, "legacy_time_flag"},
		{"inferred target", func(p *models.TradePayload) {}, 120, models.CloseReasonTP, "price_level_inferred"},
		{"planned risk with moved stop", func(p *models.TradePayload) {
			p.RiskDist = 0
			p.PlannedRiskUSDT = 20
			p.EntrySize = 4
			p.CtVal = .5
			p.MovedToBE = true
			p.BEPrice = 101
			p.StopLoss = 101
		}, 120, models.CloseReasonTP, "price_level_inferred"},
		{"ambiguous BE and TP", func(p *models.TradePayload) {
			p.MovedToBE = true
			p.BEPrice = 101
			p.StopLoss = 101
			p.TakeProfit = 104
		}, 104, models.CloseReasonUnknown, "unknown"},
		{"ambiguous lock and TP", func(p *models.TradePayload) {
			p.LockedProfit = true
			p.BEPrice = 106
			p.StopLoss = 106
			p.TakeProfit = 109
		}, 109, models.CloseReasonUnknown, "unknown"},
		{"inferred stop", func(p *models.TradePayload) {}, 90, models.CloseReasonSL, "price_level_inferred"},
		{"inferred BE", func(p *models.TradePayload) { p.MovedToBE = true; p.BEPrice = 101; p.StopLoss = 101 }, 101, models.CloseReasonBreakEven, "price_level_inferred"},
		{"inferred lock", func(p *models.TradePayload) {
			p.MovedToBE = true
			p.LockedProfit = true
			p.BEPrice = 106
			p.StopLoss = 106
		}, 106, models.CloseReasonLockProfit, "price_level_inferred"},
		{"no evidence", func(p *models.TradePayload) {}, 99, models.CloseReasonUnknown, "unknown"},
		{"invalid price", func(p *models.TradePayload) {}, math.NaN(), models.CloseReasonUnknown, "unknown"},
		{"invalid risk", func(p *models.TradePayload) { p.RiskDist = math.Inf(1) }, 120, models.CloseReasonUnknown, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := models.TradePayload{EntryPrice: 100, StopLoss: 90, TakeProfit: 120, RiskDist: 10, PosSide: "long"}
			tc.change(&p)
			reason, source := classifyCloseEvidence(p, &models.PositionTrailState{MovedToBE: true, TookPartial: true, IsStale: true}, tc.exit)
			if reason != tc.reason || source != tc.source {
				t.Fatalf("got %s/%s, want %s/%s", reason, source, tc.reason, tc.source)
			}
		})
	}
}

func TestCloseReasonDoesNotInventAnExecution(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		for _, tc := range []struct {
			name                       string
			price                      float64
			partial, be, locked, stale bool
		}{
			{name: "small loss", price: 99}, {name: "profit below target", price: 108},
			{name: "prior partial", price: 99, partial: true}, {name: "prior BE far from exit", price: 106, be: true},
			{name: "prior lock far from exit", price: 99, locked: true}, {name: "prior stale", price: 99, stale: true},
		} {
			t.Run(side+"/"+tc.name, func(t *testing.T) {
				p := models.TradePayload{EntryPrice: 100, StopLoss: 90, TakeProfit: 120, RiskDist: 10, PosSide: side, TookPartial: tc.partial, MovedToBE: tc.be, LockedProfit: tc.locked, IsStale: tc.stale, BEPrice: 101}
				price := tc.price
				if side == "short" {
					p.StopLoss = 110
					p.TakeProfit = 80
					p.BEPrice = 99
					price = 200 - price
				}
				if got := classifyCloseReason(models.TradeRecord{}, p, nil, price); got != models.CloseReasonUnknown {
					t.Fatalf("got %s, want unknown", got)
				}
			})
		}
	}
}

func TestCloseReasonLegacyTimeFlagDoesNotProveStale(t *testing.T) {
	p := models.TradePayload{EntryPrice: 100, StopLoss: 90, TakeProfit: 120, RiskDist: 10, PosSide: "long", TimeStopTriggered: true}
	if got := classifyCloseReason(models.TradeRecord{}, p, nil, 99); got != models.CloseReasonTimeStop {
		t.Fatalf("got %s, want time_stop", got)
	}
}
