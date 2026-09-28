package sessions

import (
	"testing"
	"time"
	"trade_bot/internal/models"
)

func TestLockStopAfterBreakEvenAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		side                         string
		initialSL, beSL, mfe, wantSL float64
	}{
		{"long", 90, 101, 116, 105},
		{"short", 110, 99, 84, 95},
	} {
		t.Run(tc.side, func(t *testing.T) {
			cfg := models.Settings{TrailingConfig: models.TrailingConfig{
				BETriggerR: 1, BEOffsetR: .1, LockTriggerR: 1.5, LockOffsetR: .5,
			}}
			payload := models.TradePayload{PosSide: tc.side, EntryPrice: 100, EntrySize: 2,
				StopLoss: tc.initialSL, RiskDist: 10, TakeProfit: 130}
			st := &models.PositionTrailState{PosSide: tc.side, Entry: 100, SL: tc.beSL,
				RiskDist: 10, Size: 2, MFE: tc.mfe, MovedToBE: true,
				// The persisted flag means some profit was protected, not that
				// the configured LOCK target was reached.
				LockedProfit: true, AlgoID: "be-stop"}
			now := time.Now()
			for _, recover := range []bool{false, true} {
				if recover {
					applyTrailReportState(&payload, st, 2)
					payload.MFEPrice = tc.mfe
					st = trailStateFromTrade(models.TradeRecord{InstID: "TEST-USDT-SWAP", Payload: payload})
				}
				dec := decideTrail15m(st, cfg, tc.mfe, now)
				if !dec.MoveSL || dec.NewSL != tc.wantSL || dec.Reason != models.CloseReasonLockProfit {
					t.Fatalf("recovered=%v: BE flag blocked configured LOCK: %+v", recover, dec)
				}
				if payload.EntryPrice != 100 || payload.StopLoss != tc.initialSL || payload.RiskDist != 10 {
					t.Fatal("original entry or risk was changed")
				}
			}
			st.SL = tc.wantSL
			if dec := decideTrail15m(st, cfg, tc.mfe, now); dec.MoveSL {
				t.Fatalf("already reached target replaced again: %+v", dec)
			}
			if tc.side == "long" {
				st.SL = 108
			} else {
				st.SL = 92
			}
			if dec := decideTrail15m(st, cfg, tc.mfe, now); dec.MoveSL {
				t.Fatalf("better stop was loosened: %+v", dec)
			}
		})
	}
}

func TestDecideTrail15mPrefersPartialBeforeBE(t *testing.T) {
	st := &models.PositionTrailState{
		InstID:   "BTC-USDT-SWAP",
		PosSide:  "long",
		Entry:    100,
		SL:       90,
		RiskDist: 10,
		Size:     2,
		MFE:      112,
		OpenedAt: time.Now().Add(-time.Hour),
	}

	cfg := models.Settings{
		TrailingConfig: models.TrailingConfig{
			BETriggerR:       0.8,
			BEOffsetR:        0.05,
			PartialEnabled:   true,
			PartialTriggerR:  1.1,
			PartialCloseFrac: 0.5,
		},
	}

	dec := decideTrail15m(st, cfg, 112, time.Now())
	if dec.Reason != models.CloseReasonPartialExit {
		t.Fatalf("expected partial before BE, got reason %q", dec.Reason)
	}
	if dec.CloseSize != 1 {
		t.Fatalf("expected close size 1, got %v", dec.CloseSize)
	}
	if !dec.MoveSLAfterPartial {
		t.Fatal("expected SL move after partial")
	}
}

func TestDecideTrail15mUsesRegularTimeStop(t *testing.T) {
	now := time.Now()
	st := &models.PositionTrailState{
		PosSide:  "long",
		Entry:    100,
		SL:       90,
		RiskDist: 10,
		Size:     1,
		MFE:      106,
		OpenedAt: now.Add(-13 * 15 * time.Minute),
	}
	cfg := models.Settings{TrailingConfig: models.TrailingConfig{
		TimeStopBars:        12,
		TimeStopMinCurrentR: 0.4,
	}}

	dec := decideTrail15m(st, cfg, 103, now)
	if !dec.Close || dec.Reason != models.CloseReasonTimeStop {
		t.Fatalf("expected regular time stop, got %+v", dec)
	}
}

func TestDecideTrail15mKeepsTradeAboveRegularTimeStopThreshold(t *testing.T) {
	now := time.Now()
	st := &models.PositionTrailState{
		PosSide:  "long",
		Entry:    100,
		SL:       90,
		RiskDist: 10,
		Size:     1,
		MFE:      106,
		OpenedAt: now.Add(-13 * 15 * time.Minute),
	}
	cfg := models.Settings{TrailingConfig: models.TrailingConfig{
		TimeStopBars:        12,
		TimeStopMinCurrentR: 0.4,
	}}

	dec := decideTrail15m(st, cfg, 105, now)
	if dec.Close {
		t.Fatalf("did not expect time stop above threshold, got %+v", dec)
	}
}
