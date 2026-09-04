package sessions

import (
	"context"
	"fmt"
	"math"
	"trade_bot/internal/models"
)

func (s *UserSession) CalcTradeParamsV3(
	ctx context.Context,
	signal models.Signal,
	ltf []models.CandleTick,
	htf []models.CandleTick,
) (*models.TradeParams, error) {
	cfg := s.SettingsSnapshot()
	ts := cfg.TradingSettings
	sc := s.Config.Strategy.V3

	entry := signal.Price
	if entry <= 0 {
		return nil, fmt.Errorf("entry <= 0")
	}

	instrument, err := s.Okx.GetInstrumentMeta(ctx, signal.InstID)
	if err != nil {
		return nil, fmt.Errorf("GetInstrumentMeta: %w", err)
	}

	var sl float64
	var tp float64

	switch signal.Side {
	case models.SideBuy:
		sl, err = calcLongSLV32(
			entry,
			ltf,
			instrument.TickSz,
			sc.SwingLookbackBars,
			sc.SLBufferPct,
			sc.UseATRGuard,
			sc.ATRPeriod,
			sc.ATRStopMult,
			sc.UsePercentFallback,
			sc.FallbackStopPct,
		)
		if err != nil {
			return nil, fmt.Errorf("calcLongSLV32: %w", err)
		}

		tp, err = calcLongTPV33(
			entry,
			sl,
			ltf,
			htf,
			instrument.TickSz,
			sc.MinRR,
		)
		if err != nil {
			return nil, fmt.Errorf("calcLongTPV32: %w", err)
		}

	case models.SideSell:
		sl, err = calcShortSLV32(
			entry,
			ltf,
			instrument.TickSz,
			sc.SwingLookbackBars,
			sc.SLBufferPct,
			sc.UseATRGuard,
			sc.ATRPeriod,
			sc.ATRStopMult,
			sc.UsePercentFallback,
			sc.FallbackStopPct,
		)
		if err != nil {
			return nil, fmt.Errorf("calcShortSLV32: %w", err)
		}

		tp, err = calcShortTPV33(
			entry,
			sl,
			ltf,
			htf,
			instrument.TickSz,
			sc.MinRR,
		)
		if err != nil {
			return nil, fmt.Errorf("calcShortTPV32: %w", err)
		}

	default:
		return nil, fmt.Errorf("unsupported side: %v", signal.Side)
	}

	riskDist := math.Abs(entry - sl)
	if riskDist <= 0 {
		return nil, fmt.Errorf("riskDist <= 0")
	}

	rewardDist := math.Abs(tp - entry)
	if rewardDist <= 0 {
		return nil, fmt.Errorf("rewardDist <= 0")
	}

	rrReal := rewardDist / riskDist

	const rrEps = 1e-6

	if rrReal < sc.MinRR-rrEps {
		return nil, fmt.Errorf("rr too low: got %.8f want >= %.8f", rrReal, sc.MinRR)
	}

	sizeMeta, err := s.calcSizeByRiskWithMeta(ctx, instrument, entry, sl)
	if err != nil {
		return nil, fmt.Errorf("calcSizeByRiskWithMeta: %w", err)
	}

	size := sizeMeta.NormalizedSz
	if size <= 0 {
		return nil, fmt.Errorf("size <= 0")
	}

	return &models.TradeParams{
		Entry:     entry,
		SL:        sl,
		TP:        tp,
		Size:      size,
		TickSize:  instrument.TickSz,
		RiskPct:   ts.RiskPct,
		RR:        rrReal,
		RiskDist:  riskDist,
		Leverage:  ts.Leverage,
		Direction: sideFromSignal(signal.Side),
		SizeMeta:  sizeMeta,
	}, nil
}
