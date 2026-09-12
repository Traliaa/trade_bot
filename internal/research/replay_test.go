package research

import (
	"reflect"
	"testing"
	"time"
)

func testData() (Dataset, Options) { return ExampleDataset() }
func TestSweepBOSRetestReplayCostsAndStopFirst(t *testing.T) {
	d, o := testData()
	r, err := Run(d, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Trades) != 1 {
		t.Fatalf("trades=%v funnel=%v", r.Trades, r.Funnel)
	}
	tr := r.Trades[0]
	if tr.Reason != "stop" || tr.Fees <= 0 || tr.Funding >= 0 || tr.Net >= 0 || tr.Contracts <= 0 || !tr.CloseAt.After(tr.OpenAt) {
		t.Fatalf("%+v", tr)
	}
	if tr.Risk > o.Equity*o.RiskPct/100+1e-9 {
		t.Fatal("risk exceeded")
	}
	again, err := Run(d, o)
	if err != nil || !reflect.DeepEqual(r, again) {
		t.Fatal("not reproducible", err)
	}
}
func TestFutureBarsCannotChangePastResults(t *testing.T) {
	d, o := testData()
	before, err := Run(d, o)
	if err != nil {
		t.Fatal(err)
	}
	future := d.Bars[len(d.Bars)-1]
	future.Start = o.End.Add(time.Hour)
	future.End = future.Start.Add(time.Minute)
	future.High = 10000
	d.Bars = append(d.Bars, future)
	after, err := Run(d, o)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("future leak", err)
	}
}
func TestMinimumSizeNeverRoundsRiskUp(t *testing.T) {
	d, o := testData()
	d.Snapshots[0].Candidates[0].MinSize = 100
	r, err := Run(d, o)
	if err != nil || len(r.Trades) != 0 || r.Funnel["risk_margin_or_min_size"] != 1 {
		t.Fatalf("%+v %v", r.Funnel, err)
	}
}
func TestMissingHistoricalSnapshotsAndDuplicatesFail(t *testing.T) {
	d, o := testData()
	d.Bars = append(d.Bars, d.Bars[0])
	if _, err := Run(d, o); err == nil {
		t.Fatal("duplicate accepted")
	}
	d, o = testData()
	d.Snapshots = nil
	if _, err := Run(d, o); err == nil {
		t.Fatal("missing snapshots accepted")
	}
}
func TestNoEntryAtWindowEnd(t *testing.T) {
	d, o := testData()
	o.End = o.End.Add(-time.Minute)
	r, err := Run(d, o)
	if err != nil || len(r.Trades) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}
