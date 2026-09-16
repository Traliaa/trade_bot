package models

import (
	"math"
	"time"
)

func finitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

// InitialRiskDist is for reporting; a moved protective stop is not entry risk.
func (p TradePayload) InitialRiskDist() float64 {
	if finitePositive(p.RiskDist) {
		return p.RiskDist
	}
	if p.PlannedRiskUSDT != 0 {
		if finitePositive(p.PlannedRiskUSDT) && finitePositive(p.EntrySize) && finitePositive(p.CtVal) {
			quantity := p.EntrySize * p.CtVal
			if finitePositive(quantity) {
				risk := p.PlannedRiskUSDT / quantity
				if finitePositive(risk) {
					return risk
				}
			}
		}
		return 0
	}
	if p.RiskDist != 0 {
		return 0
	}
	if p.MovedToBE || p.LockedProfit || !finitePositive(p.EntryPrice) || !finitePositive(p.StopLoss) {
		return 0
	}
	risk := CalcRiskDist(p.EntryPrice, p.StopLoss, p.PosSide)
	if finitePositive(risk) {
		return risk
	}
	return 0
}

// CalcPriceR normalizes price movement by immutable initial risk, not a live SL.
func CalcPriceR(entry, price, initialRiskDist float64, posSide string) float64 {
	if !finitePositive(entry) || !finitePositive(price) || !finitePositive(initialRiskDist) {
		return 0
	}
	var r float64
	switch posSide {
	case "long":
		r = (price - entry) / initialRiskDist
	case "short":
		r = (entry - price) / initialRiskDist
	default:
		return 0
	}
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return 0
	}
	return r
}

func CalcRiskDist(entry, stopLoss float64, posSide string) float64 {
	switch posSide {
	case "long":
		return entry - stopLoss
	case "short":
		return stopLoss - entry
	default:
		return 0
	}
}

func CalcRMultiple(entry, exit, stopLoss float64, posSide string) float64 {
	riskDist := CalcRiskDist(entry, stopLoss, posSide)
	if riskDist <= 0 {
		return 0
	}

	switch posSide {
	case "long":
		return (exit - entry) / riskDist
	case "short":
		return (entry - exit) / riskDist
	default:
		return 0
	}
}

func CalcDurationSec(entryAt time.Time, exitAt *time.Time) int64 {
	if exitAt == nil || entryAt.IsZero() || exitAt.Before(entryAt) {
		return 0
	}
	return int64(exitAt.Sub(entryAt).Seconds())
}

func CalcMFER(entry, mfePrice, stopLoss float64, posSide string) float64 {
	riskDist := CalcRiskDist(entry, stopLoss, posSide)
	if riskDist <= 0 {
		return 0
	}

	switch posSide {
	case "long":
		return (mfePrice - entry) / riskDist
	case "short":
		return (entry - mfePrice) / riskDist
	default:
		return 0
	}
}

func CalcMAER(entry, maePrice, stopLoss float64, posSide string) float64 {
	riskDist := CalcRiskDist(entry, stopLoss, posSide)
	if riskDist <= 0 {
		return 0
	}

	switch posSide {
	case "long":
		return (maePrice - entry) / riskDist
	case "short":
		return (entry - maePrice) / riskDist
	default:
		return 0
	}
}
