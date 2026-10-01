package research

import (
	"fmt"
	"math"
	"time"
)

type ExitProfile string

const (
	ExitFixed      ExitProfile = "fixed_v1"
	ExitConfigured ExitProfile = "configured_trailing_v1"
	ExitRunner     ExitProfile = "runner_3r_v1"
)

var exitProfiles = []ExitProfile{ExitFixed, ExitConfigured, ExitRunner}

type ExitState struct {
	Profile                                                                                                                                ExitProfile
	Side                                                                                                                                   string
	EntryAt, LastActionSlot, LastRunnerActionAt, StaleSince                                                                                time.Time
	Entry, InitialRiskDist, InitialContracts, ContractValue, Remaining, Stop, Target, TickSize, LotSize, MinSize, MFEPrice, StaleMarkedAtR float64
	PartialDone, BEActivated, LockedProfit, RunnerActive, IsStale                                                                          bool
}
type ExitDecision struct {
	Kind, Reason  string
	At            time.Time
	Size, NewStop float64
}

func NewExitState(s ExitSample, p ExitProfile) (ExitState, error) {
	if p != ExitFixed && p != ExitConfigured && p != ExitRunner {
		return ExitState{}, fmt.Errorf("unknown exit profile")
	}
	if why, e := validateExitSample(s, s.Config); e != nil || why != "" {
		return ExitState{}, fmt.Errorf("invalid sample: %s %v", why, e)
	}
	st := ExitState{Profile: p, Side: s.Side, EntryAt: s.EntryAt, Entry: s.Entry, InitialRiskDist: math.Abs(s.Entry - s.InitialStop), InitialContracts: s.Contracts, ContractValue: s.ContractValue, Remaining: s.Contracts, Stop: s.InitialStop, Target: s.InitialTarget, TickSize: s.TickSize, LotSize: s.LotSize, MinSize: s.MinSize, MFEPrice: s.Entry}
	if p == ExitRunner {
		st.Target = 0
	}
	return st, nil
}
func exitSlot(at time.Time) time.Time { return at.Truncate(15 * time.Minute) }
func exitRoundedStop(price, tick, sign float64) float64 {
	if sign > 0 {
		return math.Floor(price/tick+1e-9) * tick
	}
	return math.Ceil(price/tick-1e-9) * tick
}

// DecideExit observes a completed bar. Its action is applied by the executor
// at the NEXT opening; a favorable high is never an executable partial price.
func DecideExit(st ExitState, c ExitPolicyConfig, b ExitBar) (ExitState, ExitDecision, error) {
	none := ExitDecision{Kind: "none", At: b.End}
	if e := validateExitConfig(c); e != nil {
		return st, none, e
	}
	if e := validateExitBar(b); e != nil {
		return st, none, e
	}
	if (st.Profile != ExitFixed && st.Profile != ExitConfigured && st.Profile != ExitRunner) || (st.Side != "long" && st.Side != "short") || !exitPositive(st.InitialRiskDist) || !exitPositive(st.Remaining) || !exitPositive(st.Entry) || !exitPositive(st.Stop) || !exitPositive(st.TickSize) || !exitPositive(st.LotSize) || !exitPositive(st.MinSize) || b.Start.Before(st.EntryAt) {
		return st, none, fmt.Errorf("invalid policy state")
	}
	sign := exitSign(st.Side)
	r := st.InitialRiskDist
	if sign > 0 {
		st.MFEPrice = math.Max(st.MFEPrice, b.High)
	} else {
		st.MFEPrice = math.Min(st.MFEPrice, b.Low)
	}
	current := sign * (b.Close - st.Entry) / r
	mfe := sign * (st.MFEPrice - st.Entry) / r
	if !exitFinite(current) || !exitFinite(mfe) {
		return st, none, fmt.Errorf("R overflow")
	}
	closeDecision := func(reason string) ExitDecision {
		return ExitDecision{Kind: "close", Reason: reason, At: b.End, Size: st.Remaining}
	}
	stopDecision := func(px float64, reason string) ExitDecision {
		px = exitRoundedStop(px, st.TickSize, sign)
		if !exitPositive(px) || sign*(b.Close-px) <= 0 {
			d := none
			d.Reason = "stop_not_placeable"
			return d
		}
		if sign*(px-st.Stop) <= 0 {
			return none
		}
		return ExitDecision{Kind: "move_stop", Reason: reason, At: b.End, NewStop: px}
	}
	partialSize := func() float64 {
		qty := math.Floor(st.Remaining*c.PartialCloseFrac/st.LotSize+1e-9) * st.LotSize
		if !exitPositive(qty) || qty+st.LotSize*1e-9 < st.MinSize || st.Remaining-qty+st.LotSize*1e-9 < st.MinSize || qty >= st.Remaining {
			return 0
		}
		return qty
	}
	partialDecision := func(px float64) ExitDecision {
		qty := partialSize()
		if qty <= 0 {
			return none
		}
		px = exitRoundedStop(px, st.TickSize, sign)
		if sign*(px-st.Stop) < 0 {
			px = st.Stop
		}
		if !exitPositive(px) || sign*(b.Close-px) <= 0 {
			px = 0
		}
		return ExitDecision{Kind: "partial", Reason: "partial", At: b.End, Size: qty, NewStop: px}
	}
	elapsed := b.End.Sub(st.EntryAt)
	if st.Profile == ExitFixed {
		if elapsed >= time.Duration(c.FixedTimeStopBars)*15*time.Minute {
			return st, closeDecision("time_stop"), nil
		}
		return st, none, nil
	}
	if st.Profile == ExitRunner && (st.RunnerActive || current >= 3) {
		st.RunnerActive = true
		st.IsStale = false
		if !st.LastRunnerActionAt.IsZero() && !b.End.After(st.LastRunnerActionAt) {
			return st, none, nil
		}
		target := b.Close - sign*r
		if c.PartialEnabled && !st.PartialDone && current >= c.PartialTriggerR {
			if d := partialDecision(target); d.Kind != "none" {
				return st, d, nil
			}
		}
		return st, stopDecision(target, "runner_1r"), nil
	}
	if !st.LastActionSlot.IsZero() && st.LastActionSlot.Equal(exitSlot(b.End)) {
		return st, none, nil
	}
	if c.EarlyTimeStopBars > 0 && c.EarlyTimeStopMinMFER > 0 && elapsed >= time.Duration(c.EarlyTimeStopBars)*15*time.Minute && mfe < c.EarlyTimeStopMinMFER {
		return st, closeDecision("time_stop_early"), nil
	}
	if c.TimeStopBars > 0 && elapsed >= time.Duration(c.TimeStopBars)*15*time.Minute && current < c.TimeStopMinCurrentR {
		return st, closeDecision("time_stop"), nil
	}
	if !st.IsStale && elapsed >= time.Duration(c.StaleAfterBars)*15*time.Minute && mfe < c.StaleMinMFER {
		st.IsStale = true
		st.StaleSince = b.End
		st.StaleMarkedAtR = current
	}
	if st.IsStale {
		if current >= c.StaleExitProfitR {
			return st, closeDecision("stale_exit_profit"), nil
		}
		if current >= c.StaleNearBER {
			if d := stopDecision(st.Entry+sign*c.StaleTightenToBER*r, "stale_to_be"); d.Kind != "none" {
				return st, d, nil
			}
		}
		if b.End.Sub(st.StaleSince) >= time.Duration(c.StaleGraceBars)*15*time.Minute {
			if current <= c.StaleMaxAdverseR {
				return st, closeDecision("stale_max_adverse"), nil
			}
			if current <= st.StaleMarkedAtR-c.StaleWorseByR {
				return st, closeDecision("stale_degrade"), nil
			}
		}
	}
	if c.PartialEnabled && !st.PartialDone && mfe >= c.PartialTriggerR {
		if d := partialDecision(st.Entry + sign*c.BEOffsetR*r); d.Kind != "none" {
			return st, d, nil
		}
	}
	if !st.BEActivated && mfe >= c.BETriggerR {
		if d := stopDecision(st.Entry+sign*c.BEOffsetR*r, "break_even"); d.Kind != "none" {
			return st, d, nil
		}
	}
	if mfe >= c.LockTriggerR {
		px := st.Entry + sign*c.LockOffsetR*r
		if sign*(px-st.Stop) >= .1*r {
			return st, stopDecision(px, "lock_profit"), nil
		}
	}
	return st, none, nil
}
