package research

import (
	"fmt"
	"math"
	"time"
)

func ReplayExitSample(s ExitSample, m ExitManifest, p ExitProfile, mult float64) (ExitOutcome, error) {
	o := ExitOutcome{SampleID: s.ID, Profile: p, CostMultiplier: mult, Status: "censored", Ledger: []ExitLedgerEvent{}, Warnings: []string{}}
	if mult != 1 && mult != 2 {
		return o, fmt.Errorf("cost multiplier must be 1 or 2")
	}
	v, e := ValidateExitDataset(ExitDataset{Schema: 1, Provenance: s.Provenance, Manifest: m, Samples: []ExitSample{s}})
	if e != nil {
		return o, e
	}
	if len(v.Accepted) != 1 {
		return o, fmt.Errorf("sample excluded: %+v", v.Excluded)
	}
	s = v.Accepted[0]
	o.Warnings = append(o.Warnings, v.Warnings...)
	st, e := NewExitState(s, p)
	if e != nil {
		return o, e
	}
	spread := s.AssumedSpreadBPS
	if s.SpreadBPS != nil {
		spread = *s.SpreadBPS
	}
	sign := exitSign(s.Side)
	fundingIndex := 0
	fee := s.EntryFee
	if fee < 0 {
		fee *= mult
	}
	o.Ledger = append(o.Ledger, ExitLedgerEvent{At: s.EntryAt, Kind: "entry", Reason: "recorded_entry", Price: s.Entry, Size: s.Contracts, Fee: fee, RemainingAfter: s.Contracts})
	fill := func(px, qty float64, at time.Time, reason string) error {
		px *= 1 - sign*(m.Costs.SlippageBPS*mult+spread/2)/10000
		rate := m.Costs.ExitFeeBPS
		if rate > 0 {
			rate *= mult
		}
		fee := -px * qty * st.ContractValue * rate / 10000
		if e := recordExitFill(&st, &o.Ledger, at, px, qty, fee, reason); e != nil {
			return e
		}
		if st.Remaining == 0 {
			o.Status = "closed"
			t := at
			o.CloseAt = &t
			o.CloseReason = reason
		}
		return nil
	}
	fundingThrough := func(at time.Time, ambiguous bool) error {
		for fundingIndex < len(s.Funding) && !s.Funding[fundingIndex].At.After(at) {
			f := s.Funding[fundingIndex]
			fundingIndex++
			if !f.At.After(s.EntryAt) || st.Remaining <= 0 {
				continue
			}
			cash := -sign * f.Rate * f.Mark * st.Remaining * st.ContractValue
			if !exitFinite(cash) {
				return fmt.Errorf("funding overflow")
			}
			o.Ledger = append(o.Ledger, ExitLedgerEvent{At: f.At, Kind: "funding", Reason: "historical_rate", Price: f.Mark, Size: st.Remaining, Funding: cash, RemainingAfter: st.Remaining})
			if ambiguous {
				o.Warnings = append(o.Warnings, "intrabar_funding_ambiguous: charged on pre-exit size; sensitivity only")
			}
		}
		return nil
	}
	applyStop := func(px float64, at time.Time, reason string) {
		if px <= 0 || sign*(px-st.Stop) <= 0 {
			return
		}
		st.Stop = px
		// A smaller stale lock is not the configured break-even level.
		// Match sessions.approxAtOrBeyondBE, including its absolute tolerance.
		be := st.Entry + sign*m.Config.BEOffsetR*st.InitialRiskDist
		st.BEActivated = sign*(px-be) >= -1e-12
		st.LockedProfit = sign*(px-st.Entry) > 0
		o.Ledger = append(o.Ledger, ExitLedgerEvent{At: at, Kind: "stop_move", Reason: reason, Price: px, RemainingAfter: st.Remaining})
	}
	bars := append([]ExitBar{}, s.Bars...)
	if s.EntryTail != nil {
		bars = append([]ExitBar{*s.EntryTail}, bars...)
	}
	pending := ExitDecision{Kind: "none"}
	mark := s.Entry
	for _, b := range bars {
		if e = fundingThrough(b.Start, false); e != nil {
			return o, e
		}
		// A stop-only instruction was based on the previous close, never this bar's low/high.
		if pending.Kind == "move_stop" {
			applyStop(pending.NewStop, b.Start, pending.Reason)
			st.LastActionSlot = exitSlot(pending.At)
			if st.RunnerActive {
				st.LastRunnerActionAt = pending.At
			}
			pending.Kind = "none"
		}
		openStop := sign*(b.Open-st.Stop) <= 0
		openTP := st.Target > 0 && sign*(b.Open-st.Target) >= 0
		if openStop || openTP {
			px := b.Open
			reason := "stop"
			if !openStop {
				px = st.Target
				reason = "target"
			}
			if e = fill(px, st.Remaining, b.Start, reason); e != nil {
				return o, e
			}
			mark = px
			break
		}
		if pending.Kind == "partial" || pending.Kind == "close" {
			qty := st.Remaining
			if pending.Kind == "partial" {
				qty = pending.Size
			}
			if e = fill(b.Open, qty, b.Start, pending.Reason); e != nil {
				return o, e
			}
			if pending.Kind == "partial" {
				st.PartialDone = true
				if pending.NewStop > 0 && sign*(b.Open-pending.NewStop) > 0 {
					applyStop(pending.NewStop, b.Start, "after_partial")
				} else if pending.NewStop > 0 {
					o.Warnings = append(o.Warnings, "partial_stop_not_placeable_after_gap: retained previous SL")
				}
			}
			st.LastActionSlot = exitSlot(pending.At)
			if st.RunnerActive {
				st.LastRunnerActionAt = pending.At
			}
			pending.Kind = "none"
			if st.Remaining == 0 {
				mark = b.Open
				break
			}
		}
		sl, tp := b.Low <= st.Stop, st.Target > 0 && b.High >= st.Target
		if sign < 0 {
			sl, tp = b.High >= st.Stop, st.Target > 0 && b.Low <= st.Target
		}
		if e = fundingThrough(b.End, sl || tp); e != nil {
			return o, e
		}
		if sl || tp {
			if sl && tp {
				o.Ambiguities++
				o.Warnings = append(o.Warnings, "intrabar_sl_tp_ambiguous: stop-first")
			}
			px := st.Stop
			reason := "stop"
			if !sl {
				px = st.Target
				reason = "target"
			} else if sign > 0 {
				px = math.Min(px, b.Open)
			} else {
				px = math.Max(px, b.Open)
			}
			if e = fill(px, st.Remaining, b.End, reason); e != nil {
				return o, e
			}
			o.Warnings = append(o.Warnings, "protective_fill_time_modeled_at_bar_end")
			mark = px
			break
		}
		mark = b.Close
		st, pending, e = DecideExit(st, m.Config, b)
		if e != nil {
			return o, e
		}
		if pending.Reason == "stop_not_placeable" {
			o.Warnings = append(o.Warnings, "stop_not_placeable: retained previous SL")
		}
	}
	if e = summarizeExit(&o, st, mark); e != nil {
		return o, e
	}
	return o, nil
}
