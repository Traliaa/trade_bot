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

	// 1. Настройки риска
	riskPct := s.settings.TradingSettings.RiskPct / 100.0 // 3 => 0.03
	if riskPct <= 0 {
		return nil, fmt.Errorf("riskPct <= 0")
	}

	rr := s.settings.TradingSettings.TakeProfitRR
	if rr <= 0 {
		rr = 3.0
	}

	lev := s.settings.TradingSettings.Leverage
	if lev <= 0 {
		lev = 1
	}

	// 2. Мета инструмента: lastPx, lotSz, minSz, tickSz, ctVal
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

	// 3. Считаем сырой SL как процент от цены (riskPct)
	var sl float64
	if side == "BUY" {
		sl = entry * (1 - riskPct)
	} else { // SELL
		sl = entry * (1 + riskPct)
	}

	// 4. Округляем SL и TP по tickSize
	sl = roundToTick(sl, tickSz)

	// 5. 1R и TP (1R считаем уже по округлённому SL)
	riskDist := math.Abs(entry - sl)
	var tp float64
	if side == "BUY" {
		tp = entry + rr*riskDist
	} else {
		tp = entry - rr*riskDist
	}
	tp = roundToTick(tp, tickSz)

	// 6. Размер позиции с учётом реального SL и контрактной меты
	size, err := s.calcSizeByRiskWithMeta(
		ctx,
		symbol,
		entry,
		sl,
		lotSz,  // шаг sz (lotSz)
		minSz,  // минимальный sz
		tickSz, // шаг цены
		ctVal,  // номинал контракта
	)
	if err != nil {
		return nil, fmt.Errorf("calcSizeByRisk: %w", err)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size <= 0")
	}

	params := &TradeParams{
		Entry:     entry,
		SL:        sl,
		TP:        tp,
		Size:      size, // КОЛ-ВО КОНТРАКТОВ (sz) для OKX
		TickSize:  tickSz,
		RiskPct:   s.settings.TradingSettings.RiskPct,
		RR:        rr,
		RiskDist:  riskDist,
		Leverage:  lev,
		Direction: side, // "BUY"/"SELL"
	}

	return params, nil
}

func roundToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	steps := math.Round(px/tick + 1e-9)
	return steps * tick
}
