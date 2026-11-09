package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestShippedConfigsUseApprovedTradingExperiment(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"values_local.yaml", "values_ci.yaml"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("..", "..", "..", "configs", name)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}

			var cfg Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}

			if !cfg.Strategy.V3.AllowShorts {
				t.Error("strategy.v3.allow_shorts must be true")
			}
			if got := cfg.Strategy.V3.MaxOpenPositions; got != 10 {
				t.Errorf("strategy.v3.max_open_positions = %d, want 10", got)
			}
			if got := cfg.UserDefaults.DefaultMaxOpenPositions; got != 10 {
				t.Errorf("user_defaults.default_max_open_positions = %d, want 10", got)
			}
			if got := cfg.UserDefaults.DefaultMaxLongPositions; got != 5 {
				t.Errorf("user_defaults.default_max_long_positions = %d, want 5", got)
			}
			if got := cfg.UserDefaults.DefaultMaxShortPositions; got != 5 {
				t.Errorf("user_defaults.default_max_short_positions = %d, want 5", got)
			}
			if got := cfg.DefaultTrailing.EarlyTimeStopBars; got != 0 {
				t.Errorf("default_trailing.early_time_stop_bars = %d, want 0", got)
			}
			if got := cfg.DefaultTrailing.TimeStopBars; got != 0 {
				t.Errorf("default_trailing.time_stop_bars = %d, want 0", got)
			}
		})
	}
}
