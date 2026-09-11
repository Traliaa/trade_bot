package service

import (
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func passingV3Score() models.SignalScore {
	return models.SignalScore{SetupOK: true, ContextOK: true, StrongClose: true, ImpulseOK: true, StructureOK: true, Score: 7}
}

func TestV3EntryRejectReason(t *testing.T) {
	tests := []struct {
		name        string
		side        models.Side
		allowShorts bool
		opposite    int
		change      func(*models.SignalScore)
		want        models.RejectReason
	}{
		{name: "qualified disabled short", side: models.SideSell, want: models.RejectShortsDisabled},
		{name: "enabled short", side: models.SideSell, allowShorts: true},
		{name: "long unaffected by short setting", side: models.SideBuy},
		{name: "score below threshold", side: models.SideBuy, change: func(s *models.SignalScore) { s.Score = 5 }, want: models.RejectConfirmScoreLow},
		{name: "score at threshold", side: models.SideBuy, opposite: 4, change: func(s *models.SignalScore) { s.Score = 6 }},
		{name: "edge at boundary", side: models.SideBuy, opposite: 5},
		{name: "edge below boundary", side: models.SideBuy, opposite: 6, want: models.RejectScoreEdgeLow},
		{name: "tied scores", side: models.SideSell, allowShorts: true, opposite: 7, want: models.RejectScoreEdgeLow},
		{name: "disabled short still has low score", side: models.SideSell, change: func(s *models.SignalScore) { s.Score = 5 }, want: models.RejectConfirmScoreLow},
		{name: "disabled short has no edge", side: models.SideSell, opposite: 6, want: models.RejectScoreEdgeLow},
		{name: "HTF conflict despite high score", side: models.SideBuy, change: func(s *models.SignalScore) {
			s.ContextOK = false
			s.Reasons = []models.RejectReason{models.RejectHTFConflict}
		}, want: models.RejectHTFConflict},
		{name: "volume gate", side: models.SideBuy, change: func(s *models.SignalScore) {
			s.ContextOK = false
			s.Reasons = []models.RejectReason{models.RejectLowVolume}
		}, want: models.RejectLowVolume},
		{name: "missing context diagnostic", side: models.SideBuy, change: func(s *models.SignalScore) { s.ContextOK = false }, want: models.RejectInternal},
		{name: "setup gate", side: models.SideBuy, change: func(s *models.SignalScore) { s.SetupOK = false }, want: models.RejectRetestNotConfirmed},
		{name: "long close gate", side: models.SideBuy, change: func(s *models.SignalScore) { s.StrongClose = false }, want: models.RejectWeakCloseUp},
		{name: "short close gate", side: models.SideSell, change: func(s *models.SignalScore) { s.StrongClose = false }, want: models.RejectWeakCloseDown},
		{name: "weak impulse gate", side: models.SideBuy, change: func(s *models.SignalScore) { s.ImpulseOK = false }, want: models.RejectImpulseWeak},
		{name: "oversized impulse gate", side: models.SideBuy, change: func(s *models.SignalScore) {
			s.ImpulseOK = false
			s.Reasons = []models.RejectReason{models.RejectImpulseTooStrong}
		}, want: models.RejectImpulseTooStrong},
		{name: "structure gate", side: models.SideBuy, change: func(s *models.SignalScore) { s.StructureOK = false }, want: models.RejectStructureNotConfirmed},
		{name: "nonblocking diagnostic cannot hide disabled shorts", side: models.SideSell, change: func(s *models.SignalScore) { s.Reasons = []models.RejectReason{models.RejectVolatilityTooLow} }, want: models.RejectShortsDisabled},
		{name: "nonblocking diagnostic cannot hide score edge", side: models.SideBuy, opposite: 6, change: func(s *models.SignalScore) { s.Reasons = []models.RejectReason{models.RejectReclaimFailed} }, want: models.RejectScoreEdgeLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := passingV3Score()
			if tt.change != nil {
				tt.change(&s)
			}
			if got := v3EntryRejectReason(s, tt.opposite, 6, 2, tt.side, tt.allowShorts); got != tt.want {
				t.Fatalf("reason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestV3EntryRejectReasonMatchesEntryGates(t *testing.T) {
	for mask := 0; mask < 32; mask++ {
		for score := 0; score <= 9; score++ {
			for opposite := 0; opposite <= 9; opposite++ {
				for _, side := range []models.Side{models.SideBuy, models.SideSell} {
					for _, allow := range []bool{false, true} {
						s := models.SignalScore{SetupOK: mask&1 != 0, ContextOK: mask&2 != 0, StrongClose: mask&4 != 0, ImpulseOK: mask&8 != 0, StructureOK: mask&16 != 0, Score: score}
						ready := s.SetupOK && s.ContextOK && s.StrongClose && s.ImpulseOK && s.StructureOK && score >= 6 && score >= opposite+2 && (side == models.SideBuy || allow)
						reason := v3EntryRejectReason(s, opposite, 6, 2, side, allow)
						if (reason == "") != ready {
							t.Fatalf("gate mismatch: mask=%d score=%d opposite=%d side=%s allow=%v reason=%s", mask, score, opposite, side, allow, reason)
						}
					}
				}
			}
		}
	}
}

func TestV3QualifiedShortRejectionEndToEnd(t *testing.T) {
	for _, allow := range []bool{false, true} {
		cfg := &config.Config{}
		cfg.Strategy.Name = string(models.StrategyDonchianV3)
		cfg.Strategy.LTF = "15m"
		cfg.Strategy.V3.AllowShorts = allow
		cfg.Strategy.V3.MinConfirmScore = 6
		cfg.Strategy.V3.ImpulseBodyMinPct = 0.003
		cfg.Strategy.V3.ImpulseBodyMaxPct = 0.006
		cfg.Strategy.V3.VolumeMinRatio = 1
		cfg.Strategy.V3.RetestTolerancePct = 0.0015
		cfg.Strategy.V3.StrongCloseMin = 0.7
		cfg.Strategy.V3.StrongCloseMax = 0.3
		cfg.Strategy.V3.CompressionThresholdPct = 0.012
		svc := NewService(cfg, nil, nil)
		core, logs := observer.New(zap.InfoLevel)
		svc.Logger = zap.New(core)
		mst := &models.V3MarketState{LTFCandles: make([]models.CandleTick, 20), HTFCandles: make([]models.CandleTick, 10)}
		for i := range mst.LTFCandles {
			mst.LTFCandles[i] = models.CandleTick{Open: 100.1, High: 100.2, Low: 100, Close: 100.1, Volume: 100}
		}
		for i := range mst.HTFCandles {
			mst.HTFCandles[i] = models.CandleTick{High: 104, Low: 100, Close: 100.1}
		}
		last := models.CandleTick{InstID: "TEST-USDT-SWAP", Open: 100, High: 100.02, Low: 99.48, Close: 99.5, Volume: 100, End: time.Date(2026, 9, 11, 0, 15, 0, 0, time.UTC)}
		mst.LTFCandles[19] = last
		signal, ok := svc.onCandleV3ReadyLocked(last, mst)
		if ok != allow {
			t.Fatalf("allow=%v: signal=%v, reject=%s", allow, ok, svc.stateV3[last.InstID].LastRejectReason)
		}
		if allow {
			if signal.Side != models.SideSell {
				t.Fatalf("side=%s", signal.Side)
			}
			if logs.Len() != 0 {
				t.Fatal("accepted signal must not be logged as rejected")
			}
			continue
		}
		if got := svc.stateV3[last.InstID].LastRejectReason; got != models.RejectShortsDisabled {
			t.Fatalf("state reason=%s", got)
		}
		snap := svc.snapshotV3Rejects(false)
		if snap.Total != 1 || snap.Counts[models.RejectShortsDisabled] != 1 || snap.Counts[models.RejectConfirmScoreLow] != 0 {
			t.Fatalf("snapshot=%+v", snap)
		}
		entries := logs.FilterMessage("strategy reject").All()
		if len(entries) != 1 {
			t.Fatalf("INFO rejects=%d", len(entries))
		}
		fields := entries[0].ContextMap()
		for key, want := range map[string]any{"reason": "shorts_disabled", "short_entry_reject": "shorts_disabled", "candidate_side": string(models.SideSell), "allow_shorts": false, "min_confirm": int64(6), "min_score_edge": int64(2)} {
			if fields[key] != want {
				t.Errorf("%s=%v, want %v", key, fields[key], want)
			}
		}
	}
}

func TestV3NewRejectsDoNotLoosenTuning(t *testing.T) {
	for _, reason := range []models.RejectReason{models.RejectShortsDisabled, models.RejectScoreEdgeLow} {
		svc := NewService(&config.Config{}, nil, nil)
		for i := 0; i < 60; i++ {
			svc.rejects.Inc(reason)
		}
		decision := svc.AutoTuneV3Now(models.TuneManual)
		if decision.Changed || decision.Dominant != reason || decision.Total != 60 {
			t.Fatalf("reason=%s decision=%+v", reason, decision)
		}
	}
}
