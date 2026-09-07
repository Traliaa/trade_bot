package sessions

import (
	"testing"
	"trade_bot/internal/modules/config"
)

func TestEffectiveRiskPctAppliesV3Cap(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Strategy.V3.MaxRiskPct = 0.2
	sess := &UserSession{Config: cfg}

	if got := sess.EffectiveRiskPct(0.4); got != 0.2 {
		t.Fatalf("effectiveRiskPct() = %v, want 0.2", got)
	}
	if got := sess.EffectiveRiskPct(0.1); got != 0.1 {
		t.Fatalf("effectiveRiskPct() = %v, want 0.1", got)
	}
}
