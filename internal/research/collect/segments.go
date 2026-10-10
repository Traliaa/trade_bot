package collect

import (
	"errors"
	"math/big"
	"sort"
	"time"
)

type Segment struct {
	Kind  string    `json:"kind"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// PlanSegments never uses a pre-entry candle as post-entry evidence and never
// rounds the horizon. It does not imply market data for those intervals exists.
func PlanSegments(entry time.Time) ([]Segment, error) {
	if entry.IsZero() || entry.Year() < 1970 || entry.Year() > 9999 {
		return nil, errors.New("invalid_entry_time")
	}
	entry = entry.UTC()
	end := entry.Add(72 * time.Hour)
	if end.Year() > 9999 {
		return nil, errors.New("invalid_horizon")
	}
	segments := make([]Segment, 0, 4322)
	for start := entry; start.Before(end); {
		next := start.Truncate(time.Minute).Add(time.Minute)
		if next.After(end) {
			next = end
		}
		kind := "minute"
		if !start.Equal(start.Truncate(time.Minute)) || next.Sub(start) != time.Minute {
			kind = "boundary"
		}
		segments = append(segments, Segment{kind, start, next})
		start = next
	}
	return segments, nil
}

type Tick struct {
	ID    string
	At    time.Time
	Price string
}
type OHLC struct {
	Open  string `json:"open"`
	High  string `json:"high"`
	Low   string `json:"low"`
	Close string `json:"close"`
}
type BoundaryReport struct {
	Coverage string   `json:"coverage"`
	Reasons  []string `json:"reasons"`
	Trades   int      `json:"trades"`
	Prices   *OHLC    `json:"prices"`
}

// BoundaryOHLC retains unknown coverage and unknown intratimestamp ordering.
// The adapter is responsible for proving completeness by archive traversal.
func BoundaryOHLC(s Segment, ticks []Tick, sourceComplete bool) (BoundaryReport, error) {
	fail := func() (BoundaryReport, error) { return BoundaryReport{}, errors.New("invalid_boundary_evidence") }
	if s.Kind != "boundary" || s.Start.IsZero() || !s.End.After(s.Start) || s.End.Sub(s.Start) >= time.Minute || s.End.After(s.Start.Truncate(time.Minute).Add(time.Minute)) || len(ticks) > 100000 {
		return fail()
	}
	r := BoundaryReport{Coverage: "incomplete", Reasons: []string{}}
	if sourceComplete {
		r.Coverage = "complete"
	} else {
		r.Reasons = append(r.Reasons, "source_incomplete")
	}
	seen := map[string]Tick{}
	selected := make([]Tick, 0, len(ticks))
	for _, tick := range ticks {
		price, ok := decimal(tick.Price)
		if !ok || price.Sign() <= 0 || tick.ID == "" || len(tick.ID) > 128 || tick.At.IsZero() {
			return fail()
		}
		if prior, exists := seen[tick.ID]; exists {
			if !prior.At.Equal(tick.At) || !sameDecimal(prior.Price, tick.Price) {
				return fail()
			}
			continue
		}
		seen[tick.ID] = tick
		if !tick.At.Before(s.Start) && tick.At.Before(s.End) {
			selected = append(selected, tick)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if !selected[i].At.Equal(selected[j].At) {
			return selected[i].At.Before(selected[j].At)
		}
		return selected[i].ID < selected[j].ID
	})
	r.Trades = len(selected)
	if len(selected) == 0 {
		return r, nil
	}
	var high, low *big.Rat
	for i, tick := range selected {
		if i > 0 && tick.At.Equal(selected[i-1].At) && !sameDecimal(tick.Price, selected[i-1].Price) {
			r.Coverage = "incomplete"
			r.Reasons = append(r.Reasons, "tick_order_ambiguous")
			return r, nil
		}
		price, _ := decimal(tick.Price)
		if high == nil || price.Cmp(high) > 0 {
			high = price
		}
		if low == nil || price.Cmp(low) < 0 {
			low = price
		}
	}
	open, _ := decimal(selected[0].Price)
	close, _ := decimal(selected[len(selected)-1].Price)
	r.Prices = &OHLC{decimalText(open), decimalText(high), decimalText(low), decimalText(close)}
	return r, nil
}
