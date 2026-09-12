package service

import (
	"context"
	"go.uber.org/zap"
	"testing"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
)

func TestHistoricalSeedDoesNotSignalAndRetiredOneMinuteStillForwards(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategy.Name = "donchian_v3_smart"
	cfg.Strategy.LTF = "15m"
	cfg.Strategy.HTF = "1h"
	out := make(chan models.Signal, 2)
	ticks := make(chan models.CandleTick, 2)
	e := NewService(cfg, out, ticks)
	e.Base = base.New("test", zap.NewNop(), false)
	e.ConfigureUniverse(func(string) bool { return false }, func(string) bool { return false })
	e.SetWarmupDone()
	c := models.CandleTick{InstID: "RETIRED", Start: time.Now().Add(-time.Minute), End: time.Now(), TimeframeRaw: "1m", Open: 1, High: 2, Low: 1, Close: 2}
	e.SeedV3("RETIRED", []models.CandleTick{c}, []models.CandleTick{c})
	if len(out) != 0 {
		t.Fatal("history emitted signal")
	}
	e.OnTick(context.Background(), c)
	if len(ticks) != 1 {
		t.Fatal("retired position lost trailing stream")
	}
	if e.EntryAllowed("RETIRED") {
		t.Fatal("retired entry allowed")
	}
}
