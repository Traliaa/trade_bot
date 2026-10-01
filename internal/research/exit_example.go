package research

import "time"

func DefaultExitManifest() ExitManifest {
	return ExitManifest{Version: "exit-policies-v1", Costs: ExitCosts{ExitFeeBPS: 5, SlippageBPS: 3}, Config: ExitPolicyConfig{BETriggerR: 1, BEOffsetR: .1, LockTriggerR: 1.2, LockOffsetR: .6, TimeStopBars: 12, FixedTimeStopBars: 12, PartialEnabled: true, PartialTriggerR: 1.25, PartialCloseFrac: .5, StaleAfterBars: 16, StaleMinMFER: .35, StaleExitProfitR: .25, StaleNearBER: -.03, StaleMaxAdverseR: -.65, StaleGraceBars: 6, StaleWorseByR: .3, StaleTightenToBER: .05}}
}
func ExampleExitDataset() ExitDataset {
	m := DefaultExitManifest()
	at := time.Date(2025, 1, 2, 12, 0, 0, 0, time.UTC)
	spread := 2.
	s := ExitSample{ID: "synthetic-long", Symbol: "TEST-USDT-SWAP", Side: "long", Provenance: "synthetic fixture; not market performance", ReconciliationStatus: "verified", FeeCurrency: "USDT", EntryAt: at, EndAt: at.Add(3 * time.Minute), Entry: 100, InitialStop: 90, InitialTarget: 115, Contracts: 2, ContractValue: 1, TickSize: .1, LotSize: 1, MinSize: 1, EntryFee: -.1, Config: m.Config, SpreadBPS: &spread, FundingComplete: true, Funding: []Funding{}, Bars: []ExitBar{}}
	for i, p := range [][4]float64{{100, 114, 99, 113}, {113, 133, 112, 132}, {132, 133, 119, 121}} {
		start := at.Add(time.Duration(i) * time.Minute)
		s.Bars = append(s.Bars, ExitBar{Start: start, End: start.Add(time.Minute), Open: p[0], High: p[1], Low: p[2], Close: p[3], Source: "synthetic"})
	}
	return ExitDataset{Schema: 1, Provenance: "synthetic fixture; not market performance", Manifest: m, Samples: []ExitSample{s}}
}
