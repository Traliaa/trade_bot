package universe

import (
	"math"
	"testing"
	"time"
)

func fixture(now time.Time, symbol string, price float64) Candidate {
	return Candidate{Symbol: symbol, Last: price, Open: price, High: price * 1.02, Low: price * .98, BaseVolume: 20_000_000 / price, Bid: price * .9999, Ask: price * 1.0001, At: now, ListedAt: now.Add(-100 * 24 * time.Hour), Live: true, ContractValue: 1, LotSize: 1, MinSize: 1}
}
func policy() Policy {
	return Policy{Limit: 2, MinQuoteVolume: 10_000_000, MaxSpreadBPS: 20, MaxRangePct: .2, MaxMovePct: .12, MinAge: 30 * 24 * time.Hour, MaxAge: 2 * time.Minute, MinResidence: 6 * time.Hour, RetainBonus: .15}
}
func TestVolumeIsComparableAndTotalIsHardCap(t *testing.T) {
	now := time.Now()
	cs := []Candidate{fixture(now, "BTC", 60000), fixture(now, "CHEAP", .06), fixture(now, "THIRD", 4)}
	r, err := Select(now, cs, policy(), nil)
	if err != nil || len(r.Selected) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	for _, c := range r.Selected {
		if math.Abs(c.EstimatedQuoteVolume-20_000_000) > 1 {
			t.Fatal(c)
		}
	}
}
func TestEveryInstrumentMustPassSafetyFilters(t *testing.T) {
	for name, mutate := range map[string]func(*Candidate){"stale": func(c *Candidate) { c.At = c.At.Add(-time.Hour) }, "future": func(c *Candidate) { c.At = c.At.Add(time.Second) }, "new": func(c *Candidate) { c.ListedAt = c.At }, "not_live": func(c *Candidate) { c.Live = false }, "spread": func(c *Candidate) { c.Ask *= 1.1 }, "nan": func(c *Candidate) { c.BaseVolume = math.NaN() }, "inf": func(c *Candidate) { c.Last = math.Inf(1) }, "metadata": func(c *Candidate) { c.ContractValue = 0 }} {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			c := fixture(now, "BTC", 100)
			mutate(&c)
			r, err := Select(now, []Candidate{c}, policy(), nil)
			if err != nil || len(r.Selected) != 0 {
				t.Fatalf("%+v %v", r, err)
			}
		})
	}
}
func TestResidenceDoesNotOverrideSafety(t *testing.T) {
	now := time.Now()
	a := fixture(now, "A", 100)
	b := fixture(now, "B", 100)
	b.High = 110
	p := policy()
	p.Limit = 1
	r, _ := Select(now, []Candidate{a, b}, p, map[string]time.Time{"A": now.Add(-time.Hour)})
	if r.Selected[0].Symbol != "A" {
		t.Fatal(r)
	}
	a.Live = false
	r, _ = Select(now, []Candidate{a, b}, p, map[string]time.Time{"A": now})
	if r.Selected[0].Symbol != "B" {
		t.Fatal(r)
	}
}
