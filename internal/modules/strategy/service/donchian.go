package service

import (
	"math"
)

type DonchianConfig struct {
	DonPeriod     int     // LTF дончиан период, напр. 20
	MinChannelPct float64 // минимальная ширина канала (dh-dl)/close, напр. 0.004 (0.4%)
	MinImpulsePct float64 // минимальный импульс свечи (high-low)/close, напр. 0.003 (0.3%)
}

type donState struct {
	highs []float64
	lows  []float64
}

func newDonState(n int) donState {
	return donState{
		highs: make([]float64, 0, n),
		lows:  make([]float64, 0, n),
	}
}

//// OnCandle возвращает (side, reason) или SideNone
//func (s *donState) OnCandle(cfg DonchianConfig, close, high, low float64) (side string, reason string, ok bool) {
//	// нужен минимум Period+1, потому что канал считаем по предыдущим Period свечам
//	if len(s.highs) < cfg.Period {
//		// просто накапливаем
//		s.highs = append(s.highs, high)
//		s.lows = append(s.lows, low)
//		return "", "", false
//	}
//
//	// канал из ПРЕДЫДУЩИХ Period свечей: берём highs/lows как есть
//	dh := maxSlice(s.highs)
//	dl := minSlice(s.lows)
//
//	// фильтр ширины канала
//	if close > 0 {
//		widthPct := (dh - dl) / close
//		if widthPct < cfg.MinChannelPct {
//			// двигаем окно
//			s.shiftAppend(cfg.Period, high, low)
//			return "", "", false
//		}
//		impulsePct := (high - low) / close
//		if impulsePct < cfg.MinImpulsePct {
//			s.shiftAppend(cfg.Period, high, low)
//			return "", "", false
//		}
//	}
//
//	// сигнал пробоя
//	if close > dh {
//		side = "BUY"
//		reason = fmt.Sprintf("Donchian breakout UP: close=%.6f > dh=%.6f (dl=%.6f)", close, dh, dl)
//		ok = true
//	} else if close < dl {
//		side = "SELL"
//		reason = fmt.Sprintf("Donchian breakout DOWN: close=%.6f < dl=%.6f (dh=%.6f)", close, dl, dh)
//		ok = true
//	}
//
//	// двигаем окно (после расчёта)
//	s.shiftAppend(cfg.Period, high, low)
//
//	return side, reason, ok
//}

func (s *donState) shiftAppend(period int, high, low float64) {
	s.highs = append(s.highs[1:], high)
	s.lows = append(s.lows[1:], low)
	if len(s.highs) > period {
		s.highs = s.highs[len(s.highs)-period:]
		s.lows = s.lows[len(s.lows)-period:]
	}
}

func maxSlice(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minSlice(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func clampMin(v, min float64) float64 {
	return math.Max(v, min)
}
