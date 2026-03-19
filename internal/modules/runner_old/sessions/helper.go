package sessions

import (
	"fmt"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
)

func calcLongTPV33(
	entry float64,
	sl float64,
	ltf []models.CandleTick,
	htf []models.CandleTick,
	tickSize float64,
	minRR float64,
) (float64, error) {
	if entry <= 0 || sl <= 0 {
		return 0, fmt.Errorf("entry/sl <= 0")
	}
	if sl >= entry {
		return 0, fmt.Errorf("long sl >= entry")
	}

	stopDist := entry - sl
	minRRTarget := entry + minRR*stopDist

	candidates := collectLongTargets(entry, ltf, htf)
	structureTarget := nearestTargetAbove(entry, candidates)

	rawTP := minRRTarget
	if structureTarget > 0 && structureTarget > rawTP {
		rawTP = structureTarget
	}

	tp := helper.RoundUpToTick(rawTP, tickSize)
	if tp <= entry {
		return 0, fmt.Errorf("invalid long TP %.8f", tp)
	}

	return tp, nil
}
func calcShortTPV33(
	entry float64,
	sl float64,
	ltf []models.CandleTick,
	htf []models.CandleTick,
	tickSize float64,
	minRR float64,
) (float64, error) {
	if entry <= 0 || sl <= 0 {
		return 0, fmt.Errorf("entry/sl <= 0")
	}
	if sl <= entry {
		return 0, fmt.Errorf("short sl <= entry")
	}

	stopDist := sl - entry
	minRRTarget := entry - minRR*stopDist

	candidates := collectShortTargets(entry, ltf, htf)
	structureTarget := nearestTargetBelow(entry, candidates)

	rawTP := minRRTarget
	if structureTarget > 0 && structureTarget < rawTP {
		rawTP = structureTarget
	}

	tp := helper.RoundDownToTick(rawTP, tickSize)
	if tp >= entry {
		return 0, fmt.Errorf("invalid short TP %.8f", tp)
	}

	return tp, nil
}

func calcShortSLV32(
	entry float64,
	ltf []models.CandleTick,
	tickSize float64,
	lookback int,
	bufferPct float64,
	useATRGuard bool,
	atrPeriod int,
	atrStopMult float64,
	useFallback bool,
	fallbackStopPct float64,
) (float64, error) {
	if entry <= 0 {
		return 0, fmt.Errorf("entry <= 0")
	}
	if len(ltf) < 2 {
		if !useFallback {
			return 0, fmt.Errorf("not enough ltf candles for short SL")
		}
		raw := entry * (1 + fallbackStopPct)
		return helper.RoundUpToTick(raw, tickSize), nil
	}

	structureHigh := highestHighCandle(ltf, lookback)
	if structureHigh <= 0 {
		if !useFallback {
			return 0, fmt.Errorf("structureHigh <= 0")
		}
		raw := entry * (1 + fallbackStopPct)
		return helper.RoundUpToTick(raw, tickSize), nil
	}

	buffer := entry * bufferPct
	structuralSL := structureHigh + buffer

	rawSL := structuralSL

	if useATRGuard {
		atr := calcATR(ltf, atrPeriod)
		if atr > 0 {
			atrSL := entry + atr*atrStopMult
			if atrSL > rawSL {
				rawSL = atrSL
			}
		}
	}

	if rawSL <= entry {
		if !useFallback {
			return 0, fmt.Errorf("invalid short rawSL %.8f", rawSL)
		}
		rawSL = entry * (1 + fallbackStopPct)
	}

	sl := helper.RoundUpToTick(rawSL, tickSize)
	if sl <= entry {
		return 0, fmt.Errorf("invalid short SL %.8f", sl)
	}

	return sl, nil
}
func calcLongSLV32(
	entry float64,
	ltf []models.CandleTick,
	tickSize float64,
	lookback int,
	bufferPct float64,
	useATRGuard bool,
	atrPeriod int,
	atrStopMult float64,
	useFallback bool,
	fallbackStopPct float64,
) (float64, error) {
	if entry <= 0 {
		return 0, fmt.Errorf("entry <= 0")
	}
	if len(ltf) < 2 {
		if !useFallback {
			return 0, fmt.Errorf("not enough ltf candles for long SL")
		}
		raw := entry * (1 - fallbackStopPct)
		return helper.RoundDownToTick(raw, tickSize), nil
	}

	structureLow := lowestLowCandle(ltf, lookback)
	if structureLow <= 0 {
		if !useFallback {
			return 0, fmt.Errorf("structureLow <= 0")
		}
		raw := entry * (1 - fallbackStopPct)
		return helper.RoundDownToTick(raw, tickSize), nil
	}

	buffer := entry * bufferPct
	structuralSL := structureLow - buffer

	rawSL := structuralSL

	if useATRGuard {
		atr := calcATR(ltf, atrPeriod)
		if atr > 0 {
			atrSL := entry - atr*atrStopMult
			if atrSL < rawSL {
				rawSL = atrSL
			}
		}
	}

	if rawSL <= 0 || rawSL >= entry {
		if !useFallback {
			return 0, fmt.Errorf("invalid long rawSL %.8f", rawSL)
		}
		rawSL = entry * (1 - fallbackStopPct)
	}

	sl := helper.RoundDownToTick(rawSL, tickSize)
	if sl <= 0 || sl >= entry {
		return 0, fmt.Errorf("invalid long SL %.8f", sl)
	}

	return sl, nil
}

func sideFromSignal(side models.Side) string {
	switch side {
	case models.SideBuy:
		return "BUY"
	case models.SideSell:
		return "SELL"
	default:
		return ""
	}
}
func lowestLowCandle(candles []models.CandleTick, n int) float64 {
	if len(candles) == 0 {
		return 0
	}
	if n <= 0 || n > len(candles) {
		n = len(candles)
	}
	start := len(candles) - n
	low := candles[start].Low
	for i := start + 1; i < len(candles); i++ {
		if candles[i].Low < low {
			low = candles[i].Low
		}
	}
	return low
}

func highestHighCandle(candles []models.CandleTick, n int) float64 {
	if len(candles) == 0 {
		return 0
	}
	if n <= 0 || n > len(candles) {
		n = len(candles)
	}
	start := len(candles) - n
	high := candles[start].High
	for i := start + 1; i < len(candles); i++ {
		if candles[i].High > high {
			high = candles[i].High
		}
	}
	return high
}

func calcATR(candles []models.CandleTick, period int) float64 {
	if len(candles) < 2 || period <= 0 {
		return 0
	}

	if len(candles) < period+1 {
		period = len(candles) - 1
	}
	if period <= 0 {
		return 0
	}

	start := len(candles) - period
	var sum float64

	for i := start; i < len(candles); i++ {
		cur := candles[i]
		prev := candles[i-1]

		tr1 := cur.High - cur.Low
		tr2 := abs(cur.High - prev.Close)
		tr3 := abs(cur.Low - prev.Close)

		tr := tr1
		if tr2 > tr {
			tr = tr2
		}
		if tr3 > tr {
			tr = tr3
		}

		sum += tr
	}

	return sum / float64(period)
}
func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func collectShortTargets(entry float64, ltf []models.CandleTick, htf []models.CandleTick) []float64 {
	var levels []float64

	ltfLows := findSwingLows(ltf, 2)
	htfLows := findSwingLows(htf, 2)

	for _, v := range ltfLows {
		if v > 0 && v < entry {
			levels = append(levels, v)
		}
	}
	for _, v := range htfLows {
		if v > 0 && v < entry {
			levels = append(levels, v)
		}
	}

	return levels
}
func collectLongTargets(entry float64, ltf []models.CandleTick, htf []models.CandleTick) []float64 {
	var levels []float64

	ltfHighs := findSwingHighs(ltf, 2)
	htfHighs := findSwingHighs(htf, 2)

	for _, v := range ltfHighs {
		if v > entry {
			levels = append(levels, v)
		}
	}
	for _, v := range htfHighs {
		if v > entry {
			levels = append(levels, v)
		}
	}

	return levels
}
func nearestTargetAbove(entry float64, levels []float64) float64 {
	var best float64
	for _, v := range levels {
		if v <= entry {
			continue
		}
		if best == 0 || v < best {
			best = v
		}
	}
	return best
}

func nearestTargetBelow(entry float64, levels []float64) float64 {
	var best float64
	for _, v := range levels {
		if v >= entry || v <= 0 {
			continue
		}
		if best == 0 || v > best {
			best = v
		}
	}
	return best
}
func findSwingHighs(candles []models.CandleTick, leftRight int) []float64 {
	if len(candles) < 2*leftRight+1 || leftRight <= 0 {
		return nil
	}

	var out []float64

	for i := leftRight; i < len(candles)-leftRight; i++ {
		cur := candles[i].High
		ok := true

		for j := i - leftRight; j <= i+leftRight; j++ {
			if j == i {
				continue
			}
			if candles[j].High >= cur {
				ok = false
				break
			}
		}

		if ok {
			out = append(out, cur)
		}
	}

	return out
}

func findSwingLows(candles []models.CandleTick, leftRight int) []float64 {
	if len(candles) < 2*leftRight+1 || leftRight <= 0 {
		return nil
	}

	var out []float64

	for i := leftRight; i < len(candles)-leftRight; i++ {
		cur := candles[i].Low
		ok := true

		for j := i - leftRight; j <= i+leftRight; j++ {
			if j == i {
				continue
			}
			if candles[j].Low <= cur {
				ok = false
				break
			}
		}

		if ok {
			out = append(out, cur)
		}
	}

	return out
}
