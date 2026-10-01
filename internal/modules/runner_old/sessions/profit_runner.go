package sessions

import (
	"context"
	"fmt"
	"math"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	okx "trade_bot/internal/modules/okx_client/service"
)

func runnerPositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }

func runnerEligible(st *models.PositionTrailState, price float64) bool {
	if st == nil || !runnerPositive(st.Entry) || !runnerPositive(st.RiskDist) || !runnerPositive(price) || (st.PosSide != "long" && st.PosSide != "short") {
		return false
	}
	if st.ProfitRunnerActive {
		return true
	}
	if st.PosSide == "long" {
		return price-st.Entry >= 3*st.RiskDist
	}
	return st.Entry-price >= 3*st.RiskDist
}

// Operates on a detached state snapshot. Callers persist the returned state,
// including on errors after exchange acknowledgement. Never trusts cached IDs
// or a cached position size; replacement is confirmed before cancelling orders.
func reconcileRunnerProtection(ctx context.Context, client *okx.Client, st *models.PositionTrailState, checkpoint func() error) (models.Instrument, error) {
	positions, err := client.OpenPositions(ctx)
	if err != nil {
		return models.Instrument{}, err
	}
	var live *models.OpenPosition
	for i := range positions {
		if positions[i].Symbol == st.InstID && positions[i].Side == st.PosSide {
			if live != nil {
				return models.Instrument{}, fmt.Errorf("ambiguous position")
			}
			live = &positions[i]
		}
	}
	if live == nil || !runnerPositive(live.Size) {
		return models.Instrument{}, fmt.Errorf("position is no longer open")
	}
	if runnerPositive(st.Size) && live.Size > st.Size+1e-9 {
		return models.Instrument{}, fmt.Errorf("position increased externally; refusing automatic adoption")
	}
	if live.Size < st.Size-1e-9 {
		st.TookPartial = true
	}
	if st.RunnerPartialPending && live.Size < st.RunnerPartialSize-1e-9 {
		st.TookPartial = true
		st.RunnerPartialPending = false
	}
	st.Size = live.Size
	orders, err := client.PendingProtection(ctx, st.InstID, st.PosSide)
	if err != nil {
		return models.Instrument{}, err
	}
	meta, err := client.GetInstrumentMeta(ctx, st.InstID)
	if err != nil {
		return meta, err
	}
	if !runnerPositive(meta.TickSz) || !runnerEligible(st, meta.LastPx) {
		return meta, fmt.Errorf("runner threshold not reached or invalid market data")
	}
	st.TickSz = meta.TickSz
	// Keep the strongest known stop, including a manually tightened exchange SL.
	for _, o := range orders {
		if o.SL > 0 && (!runnerPositive(st.SL) || shouldImproveSL(st, o.SL)) {
			st.SL = o.SL
		}
	}
	candidate, err := runnerStop(st, meta.LastPx, meta.TickSz)
	if err != nil {
		return meta, err
	}
	// Durable mode selection precedes all exchange writes. A crash after a
	// successful replacement must never restore fixed-TP management on a dip.
	st.ProfitRunnerActive = true
	if err := checkpoint(); err != nil {
		return meta, fmt.Errorf("persist runner intent: %w", err)
	}
	keep := ""
	for _, o := range orders {
		if o.TP == 0 && math.Abs(o.Size-st.Size) < 1e-9 && math.Abs(o.SL-candidate) < meta.TickSz*1e-6 {
			keep = o.ID
			break
		}
	}
	if keep == "" {
		keep, err = client.PlaceSingleAlgo(ctx, st.InstID, st.PosSide, st.Size, candidate, false)
		if err != nil {
			return meta, err
		}
		if keep == "" {
			return meta, fmt.Errorf("empty replacement stop ID")
		}
		confirmed, err := client.PendingProtection(ctx, st.InstID, st.PosSide)
		if err != nil {
			return meta, fmt.Errorf("stop verification: %w", err)
		}
		found := false
		for _, o := range confirmed {
			if o.ID == keep && o.TP == 0 && math.Abs(o.Size-st.Size) < 1e-9 && math.Abs(o.SL-candidate) < meta.TickSz*1e-6 {
				found = true
				break
			}
		}
		if !found {
			return meta, fmt.Errorf("new stop not confirmed live; keeping old orders")
		}
	}
	st.SL = candidate
	st.AlgoID = keep
	st.ProfitRunnerActive = true
	st.LockedProfit = true
	st.MovedToBE = true
	st.IsStale = false
	if err := checkpoint(); err != nil {
		return meta, fmt.Errorf("persist confirmed stop: %w", err)
	}
	// Real live IDs only, including OCO: its SL may be cancelled only now that
	// the standalone replacement is confirmed. Errors leave remaining protection.
	for _, o := range orders {
		if o.ID == keep {
			continue
		}
		if o.TP > 0 {
			st.TPAlgoID = o.ID
		}
		if err := client.CancelAlgo(ctx, st.InstID, o.ID); err != nil {
			return meta, fmt.Errorf("cancel old protection %s: %w", o.ID, err)
		}
	}
	st.TPAlgoID = ""
	return meta, nil
}

// Initial risk and entry never follow the exchange's changed average entry.
// Round away from market; never loosen an existing more protective stop.
func runnerStop(st *models.PositionTrailState, price, tick float64) (float64, error) {
	if !runnerEligible(st, price) {
		return 0, fmt.Errorf("runner not active")
	}
	candidate := price - st.RiskDist
	if st.PosSide == "short" {
		candidate = price + st.RiskDist
	}
	if tick > 0 {
		if !runnerPositive(tick) {
			return 0, fmt.Errorf("invalid tick")
		}
		if st.PosSide == "long" {
			candidate = helper.RoundDownToTick(candidate, tick)
		} else {
			candidate = helper.RoundUpToTick(candidate, tick)
		}
	}
	if runnerPositive(st.SL) {
		if st.PosSide == "long" {
			candidate = math.Max(candidate, st.SL)
		} else {
			candidate = math.Min(candidate, st.SL)
		}
	}
	if !runnerPositive(candidate) || (st.PosSide == "long" && candidate >= price) || (st.PosSide == "short" && candidate <= price) {
		return 0, fmt.Errorf("stop reached or invalid; refusing to loosen it")
	}
	return candidate, nil
}
