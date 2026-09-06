package service

import (
	"math"
	"testing"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
)

func TestBuildMarketContextHTFBiasUsesNeutralZone(t *testing.T) {
	t.Parallel()

	svc := &Service{cfg: &config.Config{}}
	ltf := make([]models.CandleTick, 5)
	history := []models.CandleTick{
		{High: 110, Low: 95, Close: 100},
		{High: 105, Low: 90, Close: 100},
	}

	tests := []struct {
		name         string
		close        float64
		wantBias     models.MarketBias
		wantPosition float64
	}{
		{name: "below lower boundary", close: 97, wantBias: models.MarketBiasBear, wantPosition: 0.35},
		{name: "at lower boundary", close: 98, wantBias: models.MarketBiasNeutral, wantPosition: 0.40},
		{name: "above midpoint remains neutral", close: 101, wantBias: models.MarketBiasNeutral, wantPosition: 0.55},
		{name: "at upper boundary", close: 102, wantBias: models.MarketBiasNeutral, wantPosition: 0.60},
		{name: "above upper boundary", close: 103, wantBias: models.MarketBiasBull, wantPosition: 0.65},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ltfCopy := append([]models.CandleTick(nil), ltf...)
			ltfCopy[len(ltfCopy)-1].Close = tt.close
			htf := append(append([]models.CandleTick(nil), history...), models.CandleTick{Close: tt.close})

			ctx := svc.buildMarketContext(ltfCopy, htf)
			if ctx.Bias != tt.wantBias {
				t.Errorf("buildMarketContext() bias = %q, want %q", ctx.Bias, tt.wantBias)
			}
			if math.Abs(ctx.ChannelPosition-tt.wantPosition) > 1e-12 {
				t.Errorf("buildMarketContext() channel position = %f, want %f", ctx.ChannelPosition, tt.wantPosition)
			}
		})
	}
}
