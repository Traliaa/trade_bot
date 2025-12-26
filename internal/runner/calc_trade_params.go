package runner

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// calcTradeParams считает SL, TP, размер позиции и сопутствующие параметры
// по текущим настройкам стратегии.
func (s *userSession) calcTradeParams(
	ctx context.Context,
	symbol string,
	side string,
	entry float64,
) (*TradeParams, error) {
	side = strings.ToUpper(side)
	if side != "BUY" && side != "SELL" {
		return nil, fmt.Errorf("unknown side %q", side)
	}

	// RiskPct — только ДЕНЕЖНЫЙ риск (для сайзинга)
	riskPct := s.settings.TradingSettings.RiskPct / 100.0
	if riskPct <= 0 {
		return nil, fmt.Errorf("riskPct <= 0")
	}

	// StopPct — дистанция стопа (по цене)
	stopPct := s.settings.TradingSettings.StopPct / 100.0
	if stopPct <= 0 {
		return nil, fmt.Errorf("stopPct <= 0 (set TradingSettings.StopPct)")
	}

	rr := s.settings.TradingSettings.TakeProfitRR
	if rr <= 0 {
		rr = 2.0 // для 15m обычно лучше 1.5–2.5, чем 3
	}

	lev := s.settings.TradingSettings.Leverage
	if lev <= 0 {
		lev = 1
	}

	lastPx, lotSz, minSz, tickSz, ctVal, err := s.okx.GetInstrumentMeta(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("GetInstrumentMeta: %w", err)
	}
	if entry <= 0 {
		entry = lastPx
	}
	if entry <= 0 {
		return nil, fmt.Errorf("entry <= 0")
	}

	// 1) Считаем сырой SL от StopPct, а не RiskPct
	var slRaw float64
	if side == "BUY" {
		slRaw = entry * (1 - stopPct)
	} else {
		slRaw = entry * (1 + stopPct)
	}

	// 2) Округляем SL "в безопасную сторону"
	var sl float64
	if side == "BUY" {
		sl = roundDownToTick(slRaw, tickSz)
	} else {
		sl = roundUpToTick(slRaw, tickSz)
	}

	// 3) Реальная дистанция риска после округления
	riskDist := math.Abs(entry - sl)
	if riskDist <= 0 {
		return nil, fmt.Errorf("riskDist <= 0 after rounding")
	}

	// 4) TP от 1R
	var tpRaw float64
	if side == "BUY" {
		tpRaw = entry + rr*riskDist
	} else {
		tpRaw = entry - rr*riskDist
	}

	// 5) Округляем TP "в безопасную сторону"
	var tp float64
	if side == "BUY" {
		tp = roundUpToTick(tpRaw, tickSz)
	} else {
		tp = roundDownToTick(tpRaw, tickSz)
	}

	// 6) Сайзинг по ДЕНЕЖНОМУ риску RiskPct (как у тебя уже сделано)
	size, err := s.calcSizeByRiskWithMeta(
		ctx,
		symbol,
		entry,
		sl,
		lotSz,
		minSz,
		tickSz,
		ctVal,
	)
	if err != nil {
		return nil, fmt.Errorf("calcSizeByRisk: %w", err)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size <= 0")
	}

	// (опционально) полезный sanity в логи:
	// stopDistPct := riskDist / entry
	// estROEStop := stopDistPct * float64(lev) * 100.0

	return &TradeParams{
		Entry:     entry,
		SL:        sl,
		TP:        tp,
		Size:      size,
		TickSize:  tickSz,
		RiskPct:   s.settings.TradingSettings.RiskPct, // денежный риск
		RR:        rr,
		RiskDist:  riskDist, // фактический риск-ход по цене
		Leverage:  lev,
		Direction: side,
	}, nil
}

func roundDownToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	return math.Floor(px/tick+1e-12) * tick
}

func roundUpToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	return math.Ceil(px/tick-1e-12) * tick
}
