package research

import (
	"fmt"
	"sort"
	"strings"
)

type ExitComparison struct {
	ModelVersion  string          `json:"model_version"`
	DatasetSHA256 string          `json:"dataset_sha256"`
	CodeRevision  string          `json:"code_revision"`
	Provenance    string          `json:"provenance"`
	Manifest      ExitManifest    `json:"manifest"`
	Excluded      []ExitExclusion `json:"excluded"`
	Outcomes      []ExitOutcome   `json:"outcomes"`
	Cohorts       []ExitCohort    `json:"cohorts"`
	Warnings      []string        `json:"warnings"`
}
type ExitCohort struct {
	CostMultiplier float64            `json:"cost_multiplier"`
	SampleIDs      []string           `json:"sample_ids"`
	Profiles       []ExitProfileStats `json:"profiles"`
	Deltas         []ExitPairedDelta  `json:"deltas"`
	Status         string             `json:"status"`
}
type ExitProfileStats struct {
	Profile         ExitProfile `json:"profile"`
	Count           int         `json:"count"`
	Wins            int         `json:"wins"`
	Losses          int         `json:"losses"`
	Breakeven       int         `json:"breakeven"`
	Net             float64     `json:"net"`
	Fees            float64     `json:"fees"`
	Funding         float64     `json:"funding"`
	MeanNetR        float64     `json:"mean_net_r"`
	WinRate         float64     `json:"win_rate"`
	ProfitFactor    *float64    `json:"profit_factor"`
	Top1ProfitShare *float64    `json:"top1_profit_share"`
	Top3ProfitShare *float64    `json:"top3_profit_share"`
}
type ExitPairedDelta struct {
	Against        ExitProfile `json:"against"`
	SampleID       string      `json:"sample_id"`
	CostMultiplier float64     `json:"cost_multiplier"`
	NetDifference  float64     `json:"net_difference"`
	RDifference    float64     `json:"r_difference"`
}

func CompareExits(d ExitDataset, stress bool) (ExitComparison, error) {
	r := ExitComparison{ModelVersion: "exit-replay-v1", Provenance: d.Provenance, Manifest: d.Manifest, Outcomes: []ExitOutcome{}, Cohorts: []ExitCohort{}, Warnings: []string{"Independent paired exits, not portfolio/account returns; no capital or margin simulation.", "Configured profile omits V3 shared-state partial callback; not a production replica.", "OHLC path and protective fill timestamps are modeled; no guarantee of execution or profitability."}}
	v, e := ValidateExitDataset(d)
	if e != nil {
		return r, e
	}
	r.Excluded = v.Excluded
	r.Warnings = append(r.Warnings, v.Warnings...)
	if strings.Contains(strings.ToLower(d.Provenance), "synthetic") {
		r.Warnings = append(r.Warnings, "synthetic fixture; not market performance")
	}
	costs := []float64{1}
	if stress {
		costs = append(costs, 2)
	}
	for _, cost := range costs {
		cohort := ExitCohort{CostMultiplier: cost, SampleIDs: []string{}, Profiles: []ExitProfileStats{}, Deltas: []ExitPairedDelta{}, Status: "no_common_closed_samples"}
		grouped := map[ExitProfile][]ExitOutcome{}
		for _, s := range v.Accepted {
			outcomes := map[ExitProfile]ExitOutcome{}
			allClosed := true
			for _, p := range exitProfiles {
				o, e := ReplayExitSample(s, d.Manifest, p, cost)
				if e != nil {
					return r, fmt.Errorf("%s/%s: %w", s.ID, p, e)
				}
				outcomes[p] = o
				r.Outcomes = append(r.Outcomes, o)
				if o.Status != "closed" {
					allClosed = false
					r.Warnings = append(r.Warnings, fmt.Sprintf("%s/%s/x%g censored; excluded from common closed cohort", s.ID, p, cost))
				}
				for _, w := range o.Warnings {
					r.Warnings = append(r.Warnings, s.ID+"/"+string(p)+": "+w)
				}
			}
			if !allClosed {
				continue
			}
			cohort.SampleIDs = append(cohort.SampleIDs, s.ID)
			for _, p := range exitProfiles {
				grouped[p] = append(grouped[p], outcomes[p])
			}
			for _, against := range []ExitProfile{ExitFixed, ExitConfigured} {
				x := outcomes[ExitRunner]
				a := outcomes[against]
				delta := ExitPairedDelta{Against: against, SampleID: s.ID, CostMultiplier: cost, NetDifference: x.Net - a.Net, RDifference: x.NetR - a.NetR}
				if !exitFinite(delta.NetDifference) || !exitFinite(delta.RDifference) {
					return r, fmt.Errorf("paired delta overflow")
				}
				cohort.Deltas = append(cohort.Deltas, delta)
			}
		}
		if len(cohort.SampleIDs) > 0 {
			cohort.Status = "available"
			for _, p := range exitProfiles {
				stats, e := summarizeExitProfile(p, grouped[p])
				if e != nil {
					return r, e
				}
				cohort.Profiles = append(cohort.Profiles, stats)
			}
		} else {
			r.Warnings = append(r.Warnings, fmt.Sprintf("x%g: no_common_closed_samples; statistics unavailable", cost))
		}
		r.Cohorts = append(r.Cohorts, cohort)
	}
	sort.Slice(r.Outcomes, func(i, j int) bool {
		a, b := r.Outcomes[i], r.Outcomes[j]
		if a.CostMultiplier != b.CostMultiplier {
			return a.CostMultiplier < b.CostMultiplier
		}
		if a.Profile != b.Profile {
			return a.Profile < b.Profile
		}
		return a.SampleID < b.SampleID
	})
	sort.Strings(r.Warnings)
	w := []string{}
	for _, s := range r.Warnings {
		if len(w) == 0 || w[len(w)-1] != s {
			w = append(w, s)
		}
	}
	r.Warnings = w
	return r, nil
}
func summarizeExitProfile(p ExitProfile, os []ExitOutcome) (ExitProfileStats, error) {
	r := ExitProfileStats{Profile: p, Count: len(os)}
	wins := []float64{}
	profit, loss := 0., 0.
	for _, o := range os {
		for _, v := range []float64{o.Net, o.Fees, o.Funding, o.NetR} {
			if !exitFinite(v) {
				return r, fmt.Errorf("nonfinite summary input")
			}
		}
		r.Net += o.Net
		r.Fees += o.Fees
		r.Funding += o.Funding
		r.MeanNetR += o.NetR
		if o.Net > 0 {
			r.Wins++
			profit += o.Net
			wins = append(wins, o.Net)
		} else if o.Net < 0 {
			r.Losses++
			loss -= o.Net
		} else {
			r.Breakeven++
		}
		for _, v := range []float64{r.Net, r.Fees, r.Funding, r.MeanNetR, profit, loss} {
			if !exitFinite(v) {
				return r, fmt.Errorf("summary overflow")
			}
		}
	}
	if r.Count > 0 {
		r.WinRate = float64(r.Wins) / float64(r.Count)
		r.MeanNetR /= float64(r.Count)
	}
	if loss > 0 {
		pf := profit / loss
		if !exitFinite(pf) {
			return r, fmt.Errorf("profit factor overflow")
		}
		r.ProfitFactor = &pf
	}
	if profit > 0 {
		sort.Sort(sort.Reverse(sort.Float64Slice(wins)))
		top1 := wins[0] / profit
		top3 := 0.
		for i := 0; i < len(wins) && i < 3; i++ {
			top3 += wins[i]
		}
		top3 /= profit
		r.Top1ProfitShare = &top1
		r.Top3ProfitShare = &top3
	}
	return r, nil
}
