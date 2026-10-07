package sessions

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"trade_bot/internal/models"
)

// aggregateCloseExecution consumes trade-filtered, chronologically sorted fills.
// Compare exact REST decimals BEFORE converting the total to the existing float
// storage model. EntrySize is legacy float data: use its shortest round-trippable
// decimal, not a claim about independently verified original entry quantities.
func aggregateCloseExecution(fills []models.TradeFill, entrySize float64) (closedTradeExecution, error) {
	fail := func(reason string) (closedTradeExecution, error) {
		return closedTradeExecution{}, fmt.Errorf("invalid close executions: %s", reason)
	}
	if !finiteCloseNumber(entrySize) || entrySize <= 0 || len(fills) == 0 {
		return fail("invalid_entry_or_empty_fills")
	}
	entry, ok := new(big.Rat).SetString(strconv.FormatFloat(entrySize, 'f', -1, 64))
	if !ok {
		return fail("invalid_entry_size")
	}
	total := new(big.Rat)
	seen := make(map[string]models.TradeFill, len(fills))
	var result closedTradeExecution
	var notional float64
	for _, f := range fills {
		size, ok := positiveCloseDecimal(f.RawFillSize)
		if !ok || f.TradeID == "" || f.OrderID == "" || f.FillTime.IsZero() ||
			!finiteCloseNumber(f.FillPx) || f.FillPx <= 0 || !finiteCloseNumber(f.FillSz) || f.FillSz <= 0 ||
			!finiteCloseNumber(f.Fee) || !finiteCloseNumber(f.RealizedPnL) {
			return fail("invalid_fill")
		}
		parsedSize, _ := size.Float64()
		if parsedSize != f.FillSz {
			return fail("size_projection_mismatch")
		}
		if prior, found := seen[f.TradeID]; found {
			if !sameCloseFill(prior, f) {
				return fail("conflicting_duplicate")
			}
			continue
		}
		seen[f.TradeID] = f
		total.Add(total, size)
		if total.Cmp(entry) > 0 {
			return fail("exit_size_exceeds_entry")
		}
		notional += f.FillPx * f.FillSz
		result.TotalFees += f.Fee
		result.GrossRealizedPnL += f.RealizedPnL
		result.Fills = append(result.Fills, f)
		result.FinalFillPrice, result.ExitAt = f.FillPx, f.FillTime
	}
	result.ExitSize, _ = total.Float64()
	result.ExitPrice = notional / result.ExitSize
	if !finiteCloseNumber(result.ExitPrice) || result.ExitPrice <= 0 ||
		!finiteCloseNumber(result.TotalFees) || !finiteCloseNumber(result.GrossRealizedPnL) {
		return fail("nonfinite_totals")
	}
	return result, nil
}

// Restrict the rational parser to bounded plain decimal quantities, not fractions,
// exponents with unbounded allocation, signs, or nonfinite tokens.
func positiveCloseDecimal(raw string) (*big.Rat, bool) {
	if len(raw) == 0 || len(raw) > 128 {
		return nil, false
	}
	digits, dots := 0, 0
	for _, c := range raw {
		if c == '.' {
			dots++
			if dots > 1 {
				return nil, false
			}
			continue
		}
		if c < '0' || c > '9' {
			return nil, false
		}
		digits++
	}
	if digits == 0 {
		return nil, false
	}
	v, ok := new(big.Rat).SetString(raw)
	return v, ok && v.Sign() > 0
}

func finiteCloseNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// A contradictory copy must not disappear just because its side/time failed the
// close filter. IDs are scoped to instrument, so unrelated symbols do not collide.
func validateCloseFillIdentities(all, selected []models.TradeFill) error {
	type identity struct{ inst, id string }
	selectedIDs := make(map[identity]models.TradeFill, len(selected))
	for _, f := range selected {
		selectedIDs[identity{f.InstID, f.TradeID}] = f
	}
	for _, f := range all {
		if prior, found := selectedIDs[identity{f.InstID, f.TradeID}]; found && !sameCloseFill(prior, f) {
			return fmt.Errorf("invalid close executions: conflicting_duplicate")
		}
	}
	return nil
}

func sameCloseFill(a, b models.TradeFill) bool {
	as, aok := positiveCloseDecimal(a.RawFillSize)
	bs, bok := positiveCloseDecimal(b.RawFillSize)
	return aok && bok && as.Cmp(bs) == 0 && a.TradeID == b.TradeID &&
		a.OrderID == b.OrderID && a.AlgoID == b.AlgoID && a.InstID == b.InstID &&
		a.Side == b.Side && a.PosSide == b.PosSide && a.FillPx == b.FillPx &&
		a.Fee == b.Fee && a.RealizedPnL == b.RealizedPnL && a.FillTime.Equal(b.FillTime)
}
