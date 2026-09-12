package service

import (
	"testing"
	"time"
	"trade_bot/internal/modules/config"
)

func TestMarketHealthRequiresEveryTimeframe(t *testing.T) {
	s := &Service{cfg: &config.Config{Strategy: config.StrategyConfig{LTF: "15m", HTF: "1h"}}}
	now := time.Now()
	old := now.Add(-5 * time.Minute)
	s.markMarketData("1m", now)
	s.markMarketData("15m", now)
	if !s.MarketDataUpdatedAt().IsZero() {
		t.Fatal("missing HTF must not look healthy")
	}
	s.markMarketData(toOKXBar("1h"), old)
	if !s.MarketDataUpdatedAt().Equal(old) {
		t.Fatal("fresh LTF masked stale HTF")
	}
	s.markMarketData(toOKXBar("1h"), now)
	if !s.MarketDataUpdatedAt().Equal(now) {
		t.Fatal("all streams fresh")
	}
}

func TestMarketHealthDeduplicatesTimeframes(t *testing.T) {
	s := &Service{cfg: &config.Config{Strategy: config.StrategyConfig{LTF: "1m", HTF: "1m"}}}
	now := time.Now()
	s.markMarketData("1m", now)
	if !s.MarketDataUpdatedAt().Equal(now) {
		t.Fatal("duplicate timeframe required extra stream")
	}
}

func TestUniverseCountsOnlyFullyFreshSymbols(t *testing.T) {
	s := &Service{cfg: &config.Config{Strategy: config.StrategyConfig{LTF: "15m", HTF: "1h"}}, watch: []string{"BTC-USDT-SWAP", "ETH-USDT-SWAP"}, dynamicRequested: 80}
	now := time.Now()
	for _, bar := range []string{"1m", "15m", "1H"} {
		s.markMarketData(bar, now, "BTC-USDT-SWAP")
	}
	s.markMarketData("1m", now, "ETH-USDT-SWAP")
	snapshot := s.UniverseStatus(now)
	if snapshot.SelectedCount != 2 || snapshot.FreshCount != 1 || snapshot.TotalLimit != 80 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	snapshot.Symbols[0] = "mutated"
	if s.UniverseStatus(now).Symbols[0] != "BTC-USDT-SWAP" {
		t.Fatal("mutable universe snapshot")
	}
	if s.UniverseStatus(now.Add(2*time.Minute)).FreshCount != 0 {
		t.Fatal("stale streams counted as fresh")
	}
}
