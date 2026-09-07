package service

import (
	"slices"
	"testing"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
)

func TestBuildLongScoreRejectsOversizedImpulse(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Strategy.V3.ImpulseBodyMinPct = 0.003
	cfg.Strategy.V3.ImpulseBodyMaxPct = 0.006
	cfg.Strategy.V3.RetestTolerancePct = 0.0015
	cfg.Strategy.V3.StrongCloseMin = 0.7
	cfg.Strategy.V3.VolumeMinRatio = 1.0
	svc := &Service{cfg: cfg}

	candles := make([]models.CandleTick, 20)
	for i := range candles {
		candles[i] = models.CandleTick{Open: 100, High: 100.2, Low: 99.8, Close: 100, Volume: 100}
	}
	candles[len(candles)-1] = models.CandleTick{
		Open: 100, High: 101.1, Low: 99.95, Close: 101, Volume: 100,
	}

	score := svc.buildLongScore(candles, models.MarketContext{VolatilityOK: true}, 100)
	if score.ImpulseOK {
		t.Fatal("oversized impulse must not be accepted")
	}
	if !slices.Contains(score.Reasons, models.RejectImpulseTooStrong) {
		t.Fatalf("reasons = %v, want %q", score.Reasons, models.RejectImpulseTooStrong)
	}
}
