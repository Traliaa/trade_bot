package sessions

import (
	"context"
	"fmt"
	"math"
	"trade_bot/internal/models"
)

// calcSizeByRiskWithMeta считает размер позиции в КОНТРАКТАХ (sz),
// исходя из:
//   - целевого риска в USDT (RiskPct * equity),
//   - дистанции до стопа,
//   - номинала контракта (ctVal),
//   - плеча (Leverage),
//   - ограничений lotSz/minSz.
//
// ВАЖНО: для линейных USDT-SWAP на OKX:
//
//	PnL(USDT) ≈ (entry - stop) * ctVal * sz
//	margin    ≈ entry * ctVal * sz / leverage
func (s *UserSession) calcSizeByRiskWithMeta(
	ctx context.Context,
	meta models.Instrument,
	entryPrice float64,
	slPrice float64,
) (*models.SizeCalcResult, error) {
	cfg := s.SettingsSnapshot()
	ts := cfg.TradingSettings

	if entryPrice <= 0 || slPrice <= 0 {
		return nil, fmt.Errorf("entry/sl <= 0")
	}

	equity := s.RiskEquity()
	riskFraction := ts.RiskPct / 100.0
	if riskFraction <= 0 {
		return nil, fmt.Errorf("riskFraction <= 0")
	}
	riskUSDT := equity * riskFraction

	lev := float64(ts.Leverage)
	if lev <= 0 {
		lev = 1
	}

	ctVal := meta.CtVal
	if ctVal <= 0 {
		return nil, fmt.Errorf("ctVal <= 0")
	}

	stopDist := math.Abs(entryPrice - slPrice)
	if stopDist <= 0 {
		return nil, fmt.Errorf("zero stopDist")
	}

	var rawRiskSz float64
	switch meta.Kind {
	case models.ContractLinearUSDT:
		rawRiskSz = riskUSDT / (stopDist * ctVal)

	case models.ContractInverseCoin:
		a := 1.0 / entryPrice
		b := 1.0 / slPrice
		d := math.Abs(a - b)
		if d <= 0 {
			return nil, fmt.Errorf("zero inverse dist")
		}

		settlePxUSDT, err := s.Okx.SettleCcyToUSDT(ctx, meta.SettleCcy)
		if err != nil {
			return nil, fmt.Errorf("settle px: %w", err)
		}

		rawRiskSz = riskUSDT / (ctVal * d * settlePxUSDT)

	default:
		return nil, fmt.Errorf("unsupported contract kind: %v", meta.Kind)
	}

	if rawRiskSz <= 0 || math.IsNaN(rawRiskSz) || math.IsInf(rawRiskSz, 0) {
		return nil, fmt.Errorf("rawRiskSz invalid: %.10f", rawRiskSz)
	}

	rawMarginSz := (equity * lev) / (entryPrice * ctVal)
	if rawMarginSz <= 0 || math.IsNaN(rawMarginSz) || math.IsInf(rawMarginSz, 0) {
		return nil, fmt.Errorf("rawMarginSz invalid: %.10f", rawMarginSz)
	}

	rawChosenSz := math.Min(rawRiskSz, rawMarginSz)

	lotSz := meta.LotSz
	minSz := meta.MinSz
	maxMktSz := meta.MaxMktSz

	normSz, err := normalizeSize(rawChosenSz, lotSz, minSz, maxMktSz)
	if err != nil {
		return nil, err
	}

	return &models.SizeCalcResult{
		RawRiskSz:    rawRiskSz,
		RawMarginSz:  rawMarginSz,
		RawChosenSz:  rawChosenSz,
		NormalizedSz: normSz,
		RiskUSDT:     riskUSDT,
		EntryPrice:   entryPrice,
		SLPrice:      slPrice,
		StopDist:     stopDist,
		CtVal:        ctVal,
		LotSz:        lotSz,
		MinSz:        minSz,
		MaxMktSz:     maxMktSz,
	}, nil
}
