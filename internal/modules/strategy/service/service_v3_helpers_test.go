package service

import (
	"testing"
	"trade_bot/internal/models"
)

func TestWeightedRetest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		retestOK bool
		c        models.CandleTick
		level    float64
		side     models.Side
		want     int
	}{
		{
			name:     "no retest",
			retestOK: false,
			want:     0,
		},
		{
			name:     "wick-touch long",
			retestOK: true,
			c:        models.CandleTick{Low: 99.5, Close: 100.0},
			level:    100.0,
			side:     models.SideBuy,
			want:     1,
		},
		{
			name:     "close overlap long",
			retestOK: true,
			c:        models.CandleTick{Low: 100.1, Close: 101.0},
			level:    100.0,
			side:     models.SideBuy,
			want:     2,
		},
		{
			name:     "close above level long",
			retestOK: true,
			c:        models.CandleTick{Low: 99.5, Close: 100.5},
			level:    100.0,
			side:     models.SideBuy,
			want:     2,
		},
		{
			name:     "wick-touch short",
			retestOK: true,
			c:        models.CandleTick{High: 100.5, Close: 100.0},
			level:    100.0,
			side:     models.SideSell,
			want:     1,
		},
		{
			name:     "close overlap short",
			retestOK: true,
			c:        models.CandleTick{High: 99.9, Close: 99.0},
			level:    100.0,
			side:     models.SideSell,
			want:     2,
		},
		{
			name:     "close below level short",
			retestOK: true,
			c:        models.CandleTick{High: 100.5, Close: 99.5},
			level:    100.0,
			side:     models.SideSell,
			want:     2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weightedRetest(tt.retestOK, tt.c, tt.level, tt.side)
			if got != tt.want {
				t.Errorf("weightedRetest() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWeightedCloseLong(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		closePos   float64
		closeUpMin float64
		want       int
	}{
		{name: "close in middle", closePos: 0.55, closeUpMin: 0.70, want: 0},
		{name: "close in upper third", closePos: 0.75, closeUpMin: 0.70, want: 1},
		{name: "close at extreme", closePos: 0.90, closeUpMin: 0.70, want: 2},
		{name: "close at extreme boundary", closePos: 0.85, closeUpMin: 0.70, want: 2},
		{name: "close at boundary", closePos: 0.85, closeUpMin: 0.70, want: 2},
		{name: "exact on threshold", closePos: 0.70, closeUpMin: 0.70, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weightedCloseLong(tt.closePos, tt.closeUpMin)
			if got != tt.want {
				t.Errorf("weightedCloseLong() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWeightedCloseShort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		closePos   float64
		closeDnMax float64
		want       int
	}{
		{name: "close in middle", closePos: 0.55, closeDnMax: 0.30, want: 0},
		{name: "close in lower third", closePos: 0.20, closeDnMax: 0.30, want: 1},
		{name: "close at extreme", closePos: 0.10, closeDnMax: 0.30, want: 2},
		{name: "close at extreme boundary", closePos: 0.15, closeDnMax: 0.30, want: 2},
		{name: "exact on threshold", closePos: 0.30, closeDnMax: 0.30, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weightedCloseShort(tt.closePos, tt.closeDnMax)
			if got != tt.want {
				t.Errorf("weightedCloseShort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWeightedImpulse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		bodyPct    float64
		impulseMin float64
		want       int
	}{
		{name: "body below min", bodyPct: 0.002, impulseMin: 0.003, want: 0},
		{name: "body above min", bodyPct: 0.004, impulseMin: 0.003, want: 1},
		{name: "body above 2x min", bodyPct: 0.007, impulseMin: 0.003, want: 2},
		{name: "body exactly at min", bodyPct: 0.003, impulseMin: 0.003, want: 1},
		{name: "body exactly at 2x min", bodyPct: 0.006, impulseMin: 0.003, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := weightedImpulse(tt.bodyPct, tt.impulseMin)
			if got != tt.want {
				t.Errorf("weightedImpulse() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestComputeSMAVolume(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		candles []models.CandleTick
		period  int
		want    float64
	}{
		{
			name:    "empty candles",
			candles: nil,
			period:  10,
			want:    0,
		},
		{
			name: "single candle, period larger",
			candles: []models.CandleTick{
				{Volume: 100},
			},
			period: 10,
			want:   100,
		},
		{
			name: "multiple candles, exact period",
			candles: []models.CandleTick{
				{Volume: 200},
				{Volume: 100},
				{Volume: 300},
			},
			period: 3,
			want:   200, // (200+100+300)/3
		},
		{
			name: "more candles than period, uses last N",
			candles: []models.CandleTick{
				{Volume: 1000},
				{Volume: 200},
				{Volume: 100},
				{Volume: 300},
			},
			period: 2,
			want:   200, // (100+300)/2
		},
		{
			name:    "period is zero",
			candles: []models.CandleTick{{Volume: 100}},
			period:  0,
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeSMAVolume(tt.candles, tt.period)
			if got != tt.want {
				t.Errorf("computeSMAVolume() = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}
