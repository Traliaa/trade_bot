package sessions

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

// CalcTradeParams считает SL, TP, размер позиции и сопутствующие параметры
// по текущим настройкам стратегии.
func (s *UserSession) CalcTradeParams(
	ctx context.Context,
	symbol string,
	side string,
	entry float64,
) (*models.TradeParams, error) {
	cfg := s.SettingsSnapshot()
	ts := cfg.TradingSettings

	side = strings.ToUpper(side)
	if side != "BUY" && side != "SELL" {
		return nil, fmt.Errorf("unknown side %q", side)
	}

	stopPct := ts.StopPct / 100.0
	if stopPct <= 0 {
		return nil, fmt.Errorf("stopPct <= 0")
	}
	if stopPct > 0.20 {
		return nil, fmt.Errorf("stopPct too big: %.4f", stopPct)
	}

	rr := ts.TakeProfitRR
	if rr <= 0 {
		rr = 2.0
	}

	lev := ts.Leverage
	if lev <= 0 {
		lev = 1
	}

	instrument, err := s.Okx.GetInstrumentMeta(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("GetInstrumentMeta: %w", err)
	}

	if entry <= 0 {
		entry = instrument.LastPx
	}
	if entry <= 0 {
		return nil, fmt.Errorf("entry <= 0")
	}

	var slRaw float64
	if side == "BUY" {
		slRaw = entry * (1 - stopPct)
	} else {
		slRaw = entry * (1 + stopPct)
	}

	var sl float64
	if side == "BUY" {
		sl = helper.RoundDownToTick(slRaw, instrument.TickSz)
	} else {
		sl = helper.RoundUpToTick(slRaw, instrument.TickSz)
	}

	riskDist := math.Abs(entry - sl)
	if riskDist <= 0 {
		return nil, fmt.Errorf("riskDist <= 0 after rounding")
	}

	var tpRaw float64
	if side == "BUY" {
		tpRaw = entry + rr*riskDist
	} else {
		tpRaw = entry - rr*riskDist
	}

	var tp float64
	if side == "BUY" {
		tp = helper.RoundUpToTick(tpRaw, instrument.TickSz)
	} else {
		tp = helper.RoundDownToTick(tpRaw, instrument.TickSz)
	}

	sizeMeta, err := s.calcSizeByRiskWithMeta(ctx, instrument, entry, sl)
	if err != nil {
		return nil, fmt.Errorf("calcSizeByRisk: %w", err)
	}

	size := sizeMeta.NormalizedSz
	if size <= 0 {
		return nil, fmt.Errorf("size <= 0")
	}

	log.Printf(
		"[SIZE CALC] inst=%s entry=%.8f sl=%.8f riskUSDT=%.8f rawRiskSz=%.8f rawMarginSz=%.8f chosen=%.8f final=%.8f ctVal=%.8f lotSz=%.8f minSz=%.8f maxMktSz=%.8f",
		symbol,
		entry,
		sl,
		sizeMeta.RiskUSDT,
		sizeMeta.RawRiskSz,
		sizeMeta.RawMarginSz,
		sizeMeta.RawChosenSz,
		sizeMeta.NormalizedSz,
		sizeMeta.CtVal,
		sizeMeta.LotSz,
		sizeMeta.MinSz,
		sizeMeta.MaxMktSz,
	)

	return &models.TradeParams{
		Entry:     entry,
		SL:        sl,
		TP:        tp,
		Size:      size,
		TickSize:  instrument.TickSz,
		RiskPct:   ts.RiskPct,
		RR:        rr,
		RiskDist:  riskDist,
		Leverage:  lev,
		Direction: side,
		SizeMeta:  sizeMeta,
	}, nil
}

func normalizeSize(sz, lotSz, minSz, maxMktSz float64) (float64, error) {
	if sz <= 0 || math.IsNaN(sz) || math.IsInf(sz, 0) {
		return 0, fmt.Errorf("invalid raw size: %.10f", sz)
	}
	if lotSz <= 0 {
		return 0, fmt.Errorf("invalid lotSz: %.10f", lotSz)
	}
	if minSz <= 0 {
		return 0, fmt.Errorf("invalid minSz: %.10f", minSz)
	}

	steps := math.Floor(sz/lotSz + 1e-9)
	norm := steps * lotSz

	if norm < minSz {
		return 0, fmt.Errorf("normalized size %.8f below minSz %.8f", norm, minSz)
	}

	if maxMktSz > 0 && norm > maxMktSz {
		norm = math.Floor(maxMktSz/lotSz+1e-9) * lotSz
		if norm < minSz {
			return 0, fmt.Errorf("maxMktSz %.8f results in size below minSz %.8f", maxMktSz, minSz)
		}
	}

	if norm <= 0 || math.IsNaN(norm) || math.IsInf(norm, 0) {
		return 0, fmt.Errorf("invalid normalized size: %.10f", norm)
	}

	return norm, nil
}
