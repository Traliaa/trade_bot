package models

import (
	"testing"
	"trade_bot/internal/modules/config"
)

func TestNewTradingSettingsFromDefaultsUsesTimeStopMinCurrentR(t *testing.T) {
	cfg := &config.Config{}
	cfg.DefaultTrailing.TimeStopMinCurrentR = 0.25
	cfg.DefaultTrailing.EarlyTimeStopMinMFER = 0.15

	user := NewTradingSettingsFromDefaults(123, cfg)
	got := user.Settings.TrailingConfig.TimeStopMinCurrentR
	if got != 0.25 {
		t.Fatalf("expected TimeStopMinCurrentR from default trailing config, got %v", got)
	}
}
