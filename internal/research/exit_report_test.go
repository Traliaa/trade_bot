package research

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestExitComparisonCommonCohort(t *testing.T) {
	d := ExampleExitDataset()
	s := ExampleExitDataset().Samples[0]
	s.ID = "censored"
	s.Bars[2].Low = 131
	s.Bars[2].Close = 132
	d.Samples = append(d.Samples, s)
	r, e := CompareExits(d, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Outcomes) != 6 || len(r.Cohorts) != 1 || len(r.Cohorts[0].SampleIDs) != 1 {
		t.Fatal(r)
	}
	for _, p := range r.Cohorts[0].Profiles {
		if p.Count != 1 {
			t.Fatal(p)
		}
	}
	s, m := exitExecutionFixture([4]float64{100, 101, 99, 100})
	d.Manifest = m
	d.Samples = []ExitSample{s}
	r, e = CompareExits(d, false)
	if e != nil || r.Cohorts[0].Status != "no_common_closed_samples" || len(r.Cohorts[0].Profiles) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestExitComparisonMetrics(t *testing.T) {
	r, e := summarizeExitProfile(ExitFixed, []ExitOutcome{{Net: 2, NetR: .2}, {Net: -1, NetR: -.1}, {Net: 1, NetR: .1}})
	if e != nil {
		t.Fatal(e)
	}
	nearExit(t, *r.ProfitFactor, 3)
	nearExit(t, r.WinRate, 2./3)
	nearExit(t, *r.Top1ProfitShare, 2./3)
	nearExit(t, *r.Top3ProfitShare, 1)
	nearExit(t, r.MeanNetR, .2/3)
	r, e = summarizeExitProfile(ExitFixed, []ExitOutcome{{Net: 1}})
	if e != nil || r.ProfitFactor != nil {
		t.Fatal(r, e)
	}
	r, e = summarizeExitProfile(ExitFixed, []ExitOutcome{{Net: -1}})
	if e != nil || r.Top1ProfitShare != nil {
		t.Fatal(r, e)
	}
	if _, e = summarizeExitProfile(ExitFixed, []ExitOutcome{{Net: math.MaxFloat64}, {Net: math.MaxFloat64}}); e == nil {
		t.Fatal("overflow")
	}
}
func TestExitComparisonPairedStress(t *testing.T) {
	d := ExampleExitDataset()
	other := ExampleExitDataset().Samples[0]
	other.ID = "other"
	d.Samples = append(d.Samples, other)
	r, e := CompareExits(d, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Cohorts) != 2 || len(r.Outcomes) != 12 || len(r.Cohorts[0].Deltas) != 4 {
		t.Fatal(r)
	}
	d.Samples[0], d.Samples[1] = d.Samples[1], d.Samples[0]
	d.Samples[0].Bars[0], d.Samples[0].Bars[2] = d.Samples[0].Bars[2], d.Samples[0].Bars[0]
	again, e := CompareExits(d, true)
	if e != nil || !reflect.DeepEqual(r, again) {
		t.Fatal("unstable", e)
	}
}
func TestExitComparisonWarnings(t *testing.T) {
	d := ExampleExitDataset()
	d.Samples[0].FundingComplete = false
	d.Samples[0].SpreadBPS = nil
	bad := ExampleExitDataset().Samples[0]
	bad.ID = "bad"
	bad.ReconciliationStatus = "mismatch"
	d.Samples = append(d.Samples, bad)
	r, e := CompareExits(d, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Excluded) != 1 {
		t.Fatal(r)
	}
	for _, s := range []string{"funding_incomplete", "assumed_spread", "V3", "portfolio", "synthetic"} {
		if !strings.Contains(strings.Join(r.Warnings, " "), s) {
			t.Fatal("missing warning", s)
		}
	}
}
