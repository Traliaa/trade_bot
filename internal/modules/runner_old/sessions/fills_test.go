package sessions

import (
	"testing"
	"time"

	"trade_bot/internal/models"
)

func TestPickCloseFillsCollectsAndSortsAllTradeExits(t *testing.T) {
	entryAt := time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)
	tr := models.TradeRecord{
		InstID:  "BTC-USDT-SWAP",
		EntryAt: entryAt,
		Payload: models.TradePayload{PosSide: "long"},
	}
	fills := []models.TradeFill{
		{TradeID: "final", InstID: tr.InstID, PosSide: "long", Side: "sell", FillTime: entryAt.Add(3 * time.Minute)},
		{TradeID: "entry", InstID: tr.InstID, PosSide: "long", Side: "buy", FillTime: entryAt},
		{TradeID: "other-symbol", InstID: "ETH-USDT-SWAP", PosSide: "long", Side: "sell", FillTime: entryAt.Add(time.Minute)},
		{TradeID: "partial", InstID: tr.InstID, PosSide: "long", Side: "sell", FillTime: entryAt.Add(2 * time.Minute)},
		{TradeID: "old", InstID: tr.InstID, PosSide: "long", Side: "sell", FillTime: entryAt.Add(-time.Minute)},
	}

	got := pickCloseFills(fills, tr)
	if len(got) != 2 {
		t.Fatalf("expected 2 close fills, got %d: %+v", len(got), got)
	}
	if got[0].TradeID != "partial" || got[1].TradeID != "final" {
		t.Fatalf("unexpected order: %+v", got)
	}
}

func TestFillVWAP(t *testing.T) {
	fills := []models.TradeFill{
		{FillPx: 100, FillSz: 1},
		{FillPx: 103, FillSz: 2},
	}

	price, size := fillVWAP(fills)
	if size != 3 {
		t.Fatalf("expected size 3, got %v", size)
	}
	if price != 102 {
		t.Fatalf("expected VWAP 102, got %v", price)
	}
}
