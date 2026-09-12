// Package universe implements deterministic point-in-time selection.
package universe

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type Candidate struct {
	Symbol        string    `json:"symbol"`
	Last          float64   `json:"last"`
	Open          float64   `json:"open"`
	High          float64   `json:"high"`
	Low           float64   `json:"low"`
	BaseVolume    float64   `json:"base_volume"`
	Bid           float64   `json:"bid"`
	Ask           float64   `json:"ask"`
	At            time.Time `json:"at"`
	ListedAt      time.Time `json:"listed_at"`
	Live          bool      `json:"live"`
	ContractValue float64   `json:"contract_value"`
	LotSize       float64   `json:"lot_size"`
	MinSize       float64   `json:"min_size"`
	MaxLeverage   float64   `json:"max_leverage"`
}
type Policy struct {
	Limit          int           `json:"limit"`
	MinQuoteVolume float64       `json:"min_quote_volume"`
	MaxSpreadBPS   float64       `json:"max_spread_bps"`
	MaxRangePct    float64       `json:"max_range_pct"`
	MaxMovePct     float64       `json:"max_move_pct"`
	MinAge         time.Duration `json:"min_age"`
	MaxAge         time.Duration `json:"max_age"`
	MinResidence   time.Duration `json:"min_residence"`
	RetainBonus    float64       `json:"retain_bonus"`
}
type Ranked struct {
	Candidate
	EstimatedQuoteVolume float64 `json:"estimated_quote_volume"`
	SpreadBPS            float64 `json:"spread_bps"`
	Score                float64 `json:"score"`
}
type Result struct {
	Selected  []Ranked       `json:"selected"`
	Rejected  map[string]int `json:"rejected"`
	CheckedAt time.Time      `json:"checked_at"`
}

func FinitePositive(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
func (p Policy) Validate() error {
	if p.Limit < 1 || p.Limit > 500 || !FinitePositive(p.MinQuoteVolume) || !FinitePositive(p.MaxSpreadBPS) || !FinitePositive(p.MaxRangePct) || !FinitePositive(p.MaxMovePct) || p.MaxAge <= 0 || p.MinAge < 0 || p.MinResidence < 0 || p.RetainBonus < 0 || p.RetainBonus > 1 || math.IsNaN(p.RetainBonus) {
		return fmt.Errorf("invalid universe policy")
	}
	return nil
}

// Estimated quote volume is base volume times current price, not exact turnover.
func Select(now time.Time, input []Candidate, p Policy, incumbents map[string]time.Time) (Result, error) {
	r := Result{Selected: []Ranked{}, Rejected: map[string]int{}, CheckedAt: now}
	if err := p.Validate(); err != nil {
		return r, err
	}
	seen := map[string]bool{}
	for _, c := range input {
		reason := ""
		for _, v := range []float64{c.Last, c.Open, c.High, c.Low, c.BaseVolume, c.Bid, c.Ask, c.ContractValue, c.LotSize, c.MinSize} {
			if !FinitePositive(v) {
				reason = "invalid_numbers"
				break
			}
		}
		switch {
		case reason != "":
		case c.Symbol == "" || seen[c.Symbol]:
			reason = "duplicate_or_empty"
		case !c.Live:
			reason = "not_live"
		case c.At.IsZero() || c.At.After(now) || now.Sub(c.At) > p.MaxAge:
			reason = "stale_ticker"
		case c.ListedAt.IsZero() || c.ListedAt.After(now) || now.Sub(c.ListedAt) < p.MinAge:
			reason = "insufficient_age"
		case c.High < c.Low || c.High < c.Last || c.Low > c.Last || c.Ask < c.Bid:
			reason = "invalid_market"
		}
		volume := c.BaseVolume * c.Last
		spread := 10000 * (c.Ask - c.Bid) / ((c.Ask + c.Bid) / 2)
		if reason == "" {
			switch {
			case !FinitePositive(volume) || volume < p.MinQuoteVolume:
				reason = "quote_volume"
			case math.IsNaN(spread) || math.IsInf(spread, 0) || spread > p.MaxSpreadBPS:
				reason = "spread"
			case (c.High-c.Low)/c.Last > p.MaxRangePct:
				reason = "range"
			case math.Abs(c.Last-c.Open)/c.Open > p.MaxMovePct:
				reason = "move"
			}
		}
		if reason != "" {
			r.Rejected[reason]++
			continue
		}
		seen[c.Symbol] = true
		score := (c.High - c.Low) / c.Last * math.Log1p(volume) / (1 + spread/p.MaxSpreadBPS)
		if _, ok := incumbents[c.Symbol]; ok {
			score *= 1 + p.RetainBonus
		}
		r.Selected = append(r.Selected, Ranked{Candidate: c, EstimatedQuoteVolume: volume, SpreadBPS: spread, Score: score})
	}
	sort.Slice(r.Selected, func(i, j int) bool {
		a, b := r.Selected[i], r.Selected[j]
		ai, aok := incumbents[a.Symbol]
		bi, bok := incumbents[b.Symbol]
		ha := aok && !ai.After(now) && now.Sub(ai) < p.MinResidence
		hb := bok && !bi.After(now) && now.Sub(bi) < p.MinResidence
		if ha != hb {
			return ha
		}
		if a.Score == b.Score {
			return a.Symbol < b.Symbol
		}
		return a.Score > b.Score
	})
	if len(r.Selected) > p.Limit {
		r.Selected = r.Selected[:p.Limit]
	}
	return r, nil
}
