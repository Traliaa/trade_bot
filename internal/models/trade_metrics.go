package models

import "time"

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
