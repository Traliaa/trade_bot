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

	// денежный риск
	riskPct := s.settings.TradingSettings.RiskPct / 100.0
	if riskPct <= 0 {
		return nil, fmt.Errorf("riskPct <= 0")
	}

	// стоп-дистанция
	stopPct := s.settings.TradingSettings.StopPct / 100.0
	if stopPct <= 0 {
		return nil, fmt.Errorf("stopPct <= 0 (set TradingSettings.StopPct)")
	}
	// адекватный safety-guard, чтобы случайно не поставить 10% стоп
	if stopPct > 0.20 {
		return nil, fmt.Errorf("stopPct too big: %.4f", stopPct)
	}

	rr := s.settings.TradingSettings.TakeProfitRR
	if rr <= 0 {
		rr = 2.0
	}

	lev := s.settings.TradingSettings.Leverage
	if lev <= 0 {
		lev = 1
	}

	instrument, err := s.okx.GetInstrumentMeta(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("GetInstrumentMeta: %w", err)
	}

	if entry <= 0 {
		entry = instrument.LastPx
	}
	if entry <= 0 {
		return nil, fmt.Errorf("entry <= 0")
	}

	// 1) сырой SL от StopPct
	var slRaw float64
	if side == "BUY" {
		slRaw = entry * (1 - stopPct)
	} else {
		slRaw = entry * (1 + stopPct)
	}

	// 2) округляем SL "в безопасную сторону"
	// BUY: SL ниже -> roundDown
	// SELL: SL выше -> roundUp
	var sl float64
	if side == "BUY" {
		sl = roundDownToTick(slRaw, instrument.TickSz)
	} else {
		sl = roundUpToTick(slRaw, instrument.TickSz)
	}

	riskDist := math.Abs(entry - sl)
	if riskDist <= 0 {
		return nil, fmt.Errorf("riskDist <= 0 after rounding")
	}

	// 3) TP от 1R
	var tpRaw float64
	if side == "BUY" {
		tpRaw = entry + rr*riskDist
	} else {
		tpRaw = entry - rr*riskDist
	}

	// BUY: TP выше -> roundUp
	// SELL: TP ниже -> roundDown
	var tp float64
	if side == "BUY" {
		tp = roundUpToTick(tpRaw, instrument.TickSz)
	} else {
		tp = roundDownToTick(tpRaw, instrument.TickSz)
	}

	// 4) сайзинг по ДЕНЕЖНОМУ риску RiskPct
	size, err := s.calcSizeByRiskWithMeta(
		ctx,
		instrument,
		entry,
		sl,
	)
	if err != nil {
		return nil, fmt.Errorf("calcSizeByRisk: %w", err)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size <= 0")
	}

	// полезный sanity для логов:
	// stopDistPct := riskDist / entry
	// estROEStop := stopDistPct * float64(lev) * 100.0

	return &TradeParams{
		Entry:     entry,
		SL:        sl,
		TP:        tp,
		Size:      size,
		TickSize:  instrument.TickSz,
		RiskPct:   s.settings.TradingSettings.RiskPct, // денежный риск
		RR:        rr,
		RiskDist:  riskDist,
		Leverage:  lev,
		Direction: side,
	}, nil
}

func roundDownToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	steps := math.Floor(px/tick + 1e-12)
	return steps * tick
}

func roundUpToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	steps := math.Ceil(px/tick - 1e-12)
	return steps * tick
}
