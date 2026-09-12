package research

import (
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/universe"
)

// ExampleDataset is deliberately synthetic. Its output is never evidence of profitability.
func ExampleDataset() (Dataset, Options) {
	origin := time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)
	sym := "TEST-USDT-SWAP"
	bar := func(start time.Time, d time.Duration, tf string, o, h, l, c float64) models.CandleTick {
		return models.CandleTick{InstID: sym, Start: start, End: start.Add(d), TimeframeRaw: tf, Open: o, High: h, Low: l, Close: c, Volume: 100}
	}
	d := Dataset{Schema: 1, Provenance: "synthetic fixture; not market performance", FundingComplete: true}
	for i := 0; i < 5; i++ {
		d.Bars = append(d.Bars, bar(origin.Add(time.Duration(i)*time.Hour), time.Hour, "1h", 100, 102, 99, 101))
	}
	for i := 0; i < 20; i++ {
		close := 100.
		if i == 19 {
			close = 101
		}
		d.Bars = append(d.Bars, bar(origin.Add(time.Duration(i-20)*time.Hour), time.Hour, "1h", 100, 102, 99, close))
	}
	for i := 0; i < 20; i++ {
		d.Bars = append(d.Bars, bar(origin.Add(time.Duration(i)*15*time.Minute), 15*time.Minute, "15m", 100, 101, 99, 100))
	}
	d.Bars = append(d.Bars, bar(origin.Add(300*time.Minute), 15*time.Minute, "15m", 100, 100.5, 98, 100), bar(origin.Add(315*time.Minute), 15*time.Minute, "15m", 100, 102.5, 99, 102), bar(origin.Add(330*time.Minute), 15*time.Minute, "15m", 101.1, 102, 100.9, 101.5))
	at := origin.Add(345 * time.Minute)
	d.Bars = append(d.Bars, bar(at.Add(-time.Minute), time.Minute, "1m", 101.5, 101.6, 101.4, 101.5), bar(at, time.Minute, "1m", 101.5, 110, 95, 101.5))
	d.Snapshots = []Snapshot{{At: at.Add(-time.Hour), Candidates: []universe.Candidate{{Symbol: sym, Last: 100, Open: 100, High: 104, Low: 96, BaseVolume: 200000, Bid: 99.99, Ask: 100.01, At: at.Add(-time.Hour), ListedAt: origin.Add(-100 * 24 * time.Hour), Live: true, ContractValue: 1, LotSize: .001, MinSize: .001, MaxLeverage: 10}}}}
	d.Funding = []Funding{{At: at.Add(30 * time.Second), Symbol: sym, Mark: 101.5, Rate: .0001}}
	o := Options{Strategy: "smc", Start: origin.Add(300 * time.Minute), End: at.Add(time.Minute), Equity: 100, RiskPct: .2, Leverage: 3, FeeBPS: 5, SlippageBPS: 3, MaxPositions: 3, MaxHolding: time.Hour, RR: 1.5, SnapshotMaxAge: 2 * time.Hour, Policy: universe.Policy{Limit: 20, MinQuoteVolume: 10_000_000, MaxSpreadBPS: 20, MaxRangePct: .2, MaxMovePct: .12, MinAge: 30 * 24 * time.Hour, MaxAge: 2 * time.Minute}}
	return d, o
}
