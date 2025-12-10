package runner

import (
	"context"
	"fmt"
	"math"
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
func (s *userSession) calcSizeByRiskWithMeta(
	ctx context.Context,
	symbol string,
	entryPrice float64,
	slPrice float64,
	lotSz float64, // шаг sz (lotSz)
	minSz float64, // минимальный sz
	tickSize float64,
	ctVal float64, // номинал контракта (ctVal)
) (float64, error) {

	if entryPrice <= 0 || slPrice <= 0 {
		return 0, fmt.Errorf("entry/sl <= 0")
	}

	// дистанция до стопа в цене
	stopDist := math.Abs(entryPrice - slPrice)
	if stopDist <= 0 {
		return 0, fmt.Errorf("нулевой стоп")
	}

	// баланс
	equity, err := s.okx.USDTBalance(ctx)
	if err != nil {
		return 0, fmt.Errorf("get equity: %w", err)
	}
	if equity <= 0 {
		return 0, fmt.Errorf("equity <= 0")
	}

	riskFraction := s.settings.TradingSettings.RiskPct / 100.0
	if riskFraction <= 0 {
		return 0, fmt.Errorf("riskFraction <= 0")
	}
	riskUSDT := equity * riskFraction

	// sanity-check для ctVal: если по какой-то причине 0 —
	// откатываемся к старой формуле через entryPrice.
	if ctVal <= 0 {
		ctVal = 1.0
	}

	// --- 1. Размер по риску ---
	//
	// PnL(USDT) ~ stopDist * ctVal * sz
	// => szRisk = riskUSDT / (stopDist * ctVal)
	szRisk := riskUSDT / (stopDist * ctVal)
	if szRisk <= 0 || math.IsNaN(szRisk) || math.IsInf(szRisk, 0) {
		return 0, fmt.Errorf("szRisk invalid: %.8f", szRisk)
	}

	// --- 2. Ограничение по плечу / марже ---
	//
	// margin ≈ entryPrice * ctVal * sz / leverage
	// хотим margin <= equity (можно ужесточить, если нужно)
	lev := float64(s.settings.TradingSettings.Leverage)
	if lev <= 0 {
		lev = 1
	}
	maxSzByMargin := (equity * lev) / (entryPrice * ctVal)
	if maxSzByMargin <= 0 {
		return 0, fmt.Errorf("maxSzByMargin <= 0")
	}

	// берём минимум по риску и по марже
	sz := math.Min(szRisk, maxSzByMargin)

	// --- 3. Приводим к lotSz / minSz ---
	if lotSz <= 0 {
		lotSz = 1
	}
	if minSz <= 0 {
		minSz = lotSz
	}

	// округляем ВНИЗ до ближайшего шага lotSz
	steps := math.Floor(sz/lotSz + 1e-9)
	sz = steps * lotSz

	if sz < minSz {
		// если меньше минимума — поднимаем до minSz
		// (да, риск чуть превысит целевой, но иначе ордер не принять)
		sz = minSz
	}

	if sz <= 0 {
		return 0, fmt.Errorf("ноль после округления: sz=%.8f", sz)
	}

	return sz, nil
}
