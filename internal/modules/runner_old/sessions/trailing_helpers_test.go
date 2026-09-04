package sessions

import (
	"testing"
	"time"
	"trade_bot/internal/models"
)

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
