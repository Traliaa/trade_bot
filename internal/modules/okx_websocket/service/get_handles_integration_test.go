package service

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

// Explicit opt-in: public market data only; no credentials, orders or bot startup.
func TestGetCandlesOKXIntegration(t *testing.T) {
	if os.Getenv("TEST_OKX_PUBLIC_CANDLES") != "1" {
		t.Skip("set TEST_OKX_PUBLIC_CANDLES=1 to verify public OKX history")
	}
	s := &Service{client: http.Client{Timeout: 10 * time.Second}}
	for _, tc := range []struct {
		bar   string
		limit int
	}{{"1h", 230}, {"15m", 50}, {"1h", 300}} {
		candles, err := s.GetCandles(context.Background(), "RAVE-USDT-SWAP", tc.bar, tc.limit)
		if err != nil || len(candles) != tc.limit {
			t.Fatalf("%s: got %d/%d: %v", tc.bar, len(candles), tc.limit, err)
		}
		for i, candle := range candles {
			if candle.End.After(time.Now()) || (i > 0 && !candle.Start.After(candles[i-1].Start)) {
				t.Fatal("history contains future, duplicate or unordered candles")
			}
		}
		t.Logf("%s: %d closed candles, latest end %s", tc.bar, len(candles), candles[len(candles)-1].End.UTC().Format(time.RFC3339))
	}
}
