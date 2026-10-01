package sessions

import (
	"encoding/json"
	"math"
	"testing"
	"time"
	"trade_bot/internal/models"
)

func TestRunnerRecoveryPreservesModeAndPendingPartial(t *testing.T) {
	p := models.TradePayload{PosSide: "long", EntryPrice: 100, EntrySize: 2, CurrentSize: 1, StopLoss: 90, RiskDist: 10}
	st := &models.PositionTrailState{ProfitRunnerActive: true, RunnerPartialPending: true, RunnerPartialSize: 1, SL: 120, Size: 1, TookPartial: true}
	applyTrailReportState(&p, st, 1)
	raw, _ := json.Marshal(p)
	var saved models.TradePayload
	_ = json.Unmarshal(raw, &saved)
	restored := trailStateFromTrade(models.TradeRecord{Payload: saved})
	if !restored.ProfitRunnerActive || !restored.RunnerPartialPending || !restored.TookPartial || restored.RunnerPartialSize != 1 {
		t.Fatalf("lost runner state: %+v", restored)
	}
	// +3R was reached before restart. A retracement must not re-enable time exits.
	cfg := models.Settings{TrailingConfig: models.TrailingConfig{TimeStopBars: 1, TimeStopMinCurrentR: 10}}
	restored.OpenedAt = time.Now().Add(-time.Hour)
	if d := decideTrail15m(restored, cfg, 125, time.Now()); d.Close || d.CloseSize > 0 || d.MoveSL {
		t.Fatalf("runner fell back after restart: %+v", d)
	}
}

func TestRunnerStopInvalidDataAndRounding(t *testing.T) {
	for _, tc := range []struct {
		side                    string
		price, stop, tick, want float64
		bad                     bool
	}{
		{"long", 140.06, 101, .1, 130, false}, {"short", 59.94, 99, .1, 70, false},
		{"long", 140, 139, .1, 139, false}, {"short", 60, 61, .1, 61, false},
		{"long", 119, 120, .1, 0, true}, {"short", 81, 80, .1, 0, true},
		{"long", math.NaN(), 101, .1, 0, true}, {"short", math.Inf(1), 99, .1, 0, true},
	} {
		st := &models.PositionTrailState{PosSide: tc.side, Entry: 100, SL: tc.stop, RiskDist: 10, ProfitRunnerActive: true}
		got, err := runnerStop(st, tc.price, tc.tick)
		if (err != nil) != tc.bad || (!tc.bad && math.Abs(got-tc.want) > 1e-9) {
			t.Fatalf("%+v got=%v err=%v", tc, got, err)
		}
	}
}

func TestRunnerGuardStillRequiresStop(t *testing.T) {
	if got := missingProtection(false, true, true); len(got) != 0 {
		t.Fatalf("SL-only runner warns about TP: %v", got)
	}
	if got := missingProtection(false, false, true); len(got) != 1 || got[0] != "SL" {
		t.Fatalf("missing SL hidden: %v", got)
	}
	if got := missingProtection(false, true, false); len(got) != 1 || got[0] != "TP" {
		t.Fatalf("unmanaged missing TP hidden: %v", got)
	}
}

func TestManualSnapshotWaitsForRunnerExecution(t *testing.T) {
	key := models.PosKey{InstID: "AVAX-USDT-SWAP", PosSide: "long"}
	st := &models.PositionTrailState{Size: 2}
	s := &UserSession{TrailStates: map[models.PosKey]*models.PositionTrailState{key: st}}
	s.TrailExecMu.Lock()
	started, done := make(chan struct{}), make(chan struct{})
	go func() { close(started); s.ApplyManualPositionSnapshot(key.InstID, key.PosSide, 1); close(done) }()
	<-started
	select {
	case <-done:
		s.TrailExecMu.Unlock()
		t.Fatal("manual snapshot raced runner checkpoint")
	case <-time.After(50 * time.Millisecond):
	}
	s.TrailExecMu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("manual snapshot blocked after runner finished")
	}
	if st.Size != 1 || !st.TookPartial {
		t.Fatal("manual residual lost")
	}
}

// A fixed entry-relative LOCK or a 15m slot gate must not replace the
// agreed current-price-minus-1R trailing rule after +3R.
func TestProfitRunnerDecision(t *testing.T) {
	for _, tc := range []struct {
		side            string
		price, sl, want float64
	}{
		{"long", 130, 101, 120}, {"short", 70, 99, 80},
		{"long", 150, 120, 140}, {"short", 50, 80, 60},
	} {
		t.Run(tc.side, func(t *testing.T) {
			now := time.Date(2026, 10, 1, 12, 1, 0, 0, time.UTC)
			st := &models.PositionTrailState{PosSide: tc.side, Entry: 100, SL: tc.sl, RiskDist: 10, Size: 1, MFE: tc.price, MovedToBE: true, TookPartial: true, LastTrailEnd: now.Truncate(15 * time.Minute)}
			d := decideTrail15m(st, models.Settings{}, tc.price, now)
			if !d.MoveSL || d.NewSL != tc.want || d.Close {
				t.Fatalf("want SL %v, got %+v", tc.want, d)
			}
			st.SL = tc.want
			price := tc.price - 2
			if tc.side == "short" {
				price = tc.price + 2
			}
			if d = decideTrail15m(st, models.Settings{}, price, now.Add(time.Minute)); d.MoveSL || d.Close {
				t.Fatalf("retracement loosened stop: %+v", d)
			}
		})
	}
}

func TestProfitRunnerPartialProtectsRemainderAtCurrentPrice(t *testing.T) {
	st := &models.PositionTrailState{PosSide: "long", Entry: 100, SL: 90, RiskDist: 10, Size: 2, MFE: 140}
	cfg := models.Settings{TrailingConfig: models.TrailingConfig{PartialEnabled: true, PartialTriggerR: 1.25, PartialCloseFrac: .5}}
	d := decideTrail15m(st, cfg, 140, time.Now())
	if d.CloseSize != 1 || d.NewSLAfterPartial != 130 {
		t.Fatalf("partial lost runner protection: %+v", d)
	}
}

func TestRecoveryDoesNotRepeatRecordedPartial(t *testing.T) {
	p := models.TradePayload{PosSide: "long", EntryPrice: 100, StopLoss: 90, EntrySize: 2, CurrentSize: 1, RiskDist: 10, PartialCount: 1}
	st := trailStateFromTrade(models.TradeRecord{Payload: p})
	cfg := models.Settings{TrailingConfig: models.TrailingConfig{PartialEnabled: true, PartialTriggerR: 1, PartialCloseFrac: .5}}
	if d := decideTrail15m(st, cfg, 140, time.Now()); d.CloseSize > 0 {
		t.Fatalf("recovery repeated partial: %+v", d)
	}
}
