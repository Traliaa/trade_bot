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
) (float64, error) {

	if entryPrice <= 0 || slPrice <= 0 {
		return 0, fmt.Errorf("entry/sl <= 0")
	}

	equity, err := s.Okx.USDTBalance(ctx)
	if err != nil {
		return 0, fmt.Errorf("get equity: %w", err)
	}
	if equity <= 0 {
		return 0, fmt.Errorf("equity <= 0")
	}

	riskFraction := s.Settings.TradingSettings.RiskPct / 100.0
	if riskFraction <= 0 {
		return 0, fmt.Errorf("riskFraction <= 0")
	}
	riskUSDT := equity * riskFraction

	// leverage cap
	lev := float64(s.Settings.TradingSettings.Leverage)
	if lev <= 0 {
		lev = 1
	}

	ctVal := meta.CtVal
	if ctVal <= 0 {
		return 0, fmt.Errorf("ctVal <= 0")
	}

	// 1) риск по формуле
	var szRisk float64
	switch meta.Kind {
	case models.ContractLinearUSDT:
		stopDist := math.Abs(entryPrice - slPrice)
		if stopDist <= 0 {
			return 0, fmt.Errorf("zero stopDist")
		}
		szRisk = riskUSDT / (stopDist * ctVal)

	case models.ContractInverseCoin:
		// pnl_coin_per_sz = ctVal * abs(1/entry - 1/sl)
		a := 1.0 / entryPrice
		b := 1.0 / slPrice
		d := math.Abs(a - b)
		if d <= 0 {
			return 0, fmt.Errorf("zero inverse dist")
		}

		settlePxUSDT, err := s.Okx.SettleCcyToUSDT(ctx, meta.SettleCcy)
		if err != nil {
			return 0, fmt.Errorf("settle px: %w", err)
		}

		// riskUSDT = sz * ctVal * d * settlePxUSDT
		szRisk = riskUSDT / (ctVal * d * settlePxUSDT)

	default:
		return 0, fmt.Errorf("unsupported contract kind: %v (ctType=%s settle=%s ctValCcy=%s)",
			meta.Kind, meta.Kind, meta.SettleCcy, meta.CtValCcy)
	}

	if szRisk <= 0 || math.IsNaN(szRisk) || math.IsInf(szRisk, 0) {
		return 0, fmt.Errorf("szRisk invalid: %.10f", szRisk)
	}

	// 2) ограничение по марже (приближенно)
	var maxSzByMargin float64
	switch meta.Kind {
	case models.ContractLinearUSDT:
		// margin ≈ entry * ctVal * sz / lev
		maxSzByMargin = (equity * lev) / (entryPrice * ctVal)

	case models.ContractInverseCoin:
		// для inverse это приблизительно, потому что маржа в coin,
		// но как safe-cap можно так же ограничить через USDT оценку.
		// marginUSDT ≈ entry * ctVal * sz / lev (оценка)
		maxSzByMargin = (equity * lev) / (entryPrice * ctVal)
	}

	if maxSzByMargin <= 0 || math.IsNaN(maxSzByMargin) || math.IsInf(maxSzByMargin, 0) {
		return 0, fmt.Errorf("maxSzByMargin invalid: %.10f", maxSzByMargin)
	}

	sz := math.Min(szRisk, maxSzByMargin)

	// 3) cap по MaxMktSz (если есть)
	if meta.MaxMktSz > 0 && sz > meta.MaxMktSz {
		sz = meta.MaxMktSz
	}

	// 4) округление под lotSz/minSz
	lotSz := meta.LotSz
	minSz := meta.MinSz
	if lotSz <= 0 {
		lotSz = 1
	}
	if minSz <= 0 {
		minSz = lotSz
	}

	steps := math.Floor(sz/lotSz + 1e-9)
	sz = steps * lotSz
	if sz < minSz {
		sz = minSz
	}

	// после поднятия до minSz — ещё раз проверим MaxMktSz
	if meta.MaxMktSz > 0 && sz > meta.MaxMktSz {
		return 0, fmt.Errorf("minSz > maxMktSz: minSz=%.8f maxMktSz=%.8f", minSz, meta.MaxMktSz)
	}

	if sz <= 0 {
		return 0, fmt.Errorf("sz <= 0 after rounding: %.10f", sz)
	}

	return sz, nil
}
