package research

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func exitExecutionFixture(rows ...[4]float64) (ExitSample, ExitManifest) {
	d := ExampleExitDataset()
	s := d.Samples[0]
	s.Bars = nil
	s.EndAt = s.EntryAt.Add(time.Duration(len(rows)) * time.Minute)
	z := 0.
	s.SpreadBPS = &z
	s.EntryFee = 0
	d.Manifest.Costs = ExitCosts{}
	for i, p := range rows {
		at := s.EntryAt.Add(time.Duration(i) * time.Minute)
		s.Bars = append(s.Bars, ExitBar{Start: at, End: at.Add(time.Minute), Open: p[0], High: p[1], Low: p[2], Close: p[3]})
	}
	return s, d.Manifest
}
func nearExit(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-8 {
		t.Fatalf("got %.10f want %.10f", got, want)
	}
}
func TestExitExecutionLedgerConservation(t *testing.T) {
	s, m := exitExecutionFixture([4]float64{100, 114, 99, 113}, [4]float64{112.5, 131, 112, 130}, [4]float64{121, 121, 119, 120})
	s.EntryFee = -.1
	m.Costs.ExitFeeBPS = 5
	s.Funding = []Funding{{At: s.EntryAt.Add(2 * time.Minute), Symbol: s.Symbol, Mark: 120, Rate: .001}}
	o, e := ReplayExitSample(s, m, ExitRunner, 1)
	if e != nil {
		t.Fatal(e)
	}
	nearExit(t, o.Gross, 32.5)
	nearExit(t, o.Fees, -.21625)
	nearExit(t, o.Funding, -.12)
	nearExit(t, o.Net, 32.16375)
	nearExit(t, o.NetR, 1.6081875)
	if o.Status != "closed" || o.Remaining != 0 {
		t.Fatal(o)
	}
	st, _ := NewExitState(s, ExitRunner)
	events := []ExitLedgerEvent{}
	if e := recordExitFill(&st, &events, s.EntryAt, 100, 3, 0, "exit"); e == nil {
		t.Fatal("overclose")
	}
	if e := recordExitFill(&st, &events, s.EntryAt, 100, 1, .01, "partial"); e != nil {
		t.Fatal(e)
	}
	nearExit(t, events[0].Fee, .01)
}
func TestExitExecutionNoRetroactiveStop(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		s, m := exitExecutionFixture([4]float64{100, 131, 95, 130}, [4]float64{129, 130, 119, 125})
		s.Config.PartialEnabled = false
		m.Config = s.Config
		if side == "short" {
			s.Side = side
			s.InitialStop = 110
			s.InitialTarget = 85
			for i, b := range s.Bars {
				s.Bars[i].Open = 200 - b.Open
				s.Bars[i].High = 200 - b.Low
				s.Bars[i].Low = 200 - b.High
				s.Bars[i].Close = 200 - b.Close
			}
		}
		o, e := ReplayExitSample(s, m, ExitRunner, 1)
		if e != nil {
			t.Fatal(e)
		}
		if o.Status != "closed" || !o.CloseAt.Equal(s.EndAt) {
			t.Fatalf("retroactive %s %+v", side, o)
		}
		nearExit(t, o.Gross, 40)
	}
}
func TestExitExecutionStopFirstAndGap(t *testing.T) {
	for _, tt := range []struct {
		rows  [][4]float64
		gross float64
		amb   int
	}{{[][4]float64{{100, 116, 89, 110}}, -20, 1}, {[][4]float64{{100, 101, 99, 100}, {85, 86, 84, 85}}, -30, 0}} {
		s, m := exitExecutionFixture(tt.rows...)
		o, e := ReplayExitSample(s, m, ExitFixed, 1)
		if e != nil {
			t.Fatal(e)
		}
		nearExit(t, o.Gross, tt.gross)
		if o.CloseReason != "stop" || o.Ambiguities != tt.amb {
			t.Fatal(o)
		}
	}
}
func TestExitExecutionPendingPartialVsStop(t *testing.T) {
	s, m := exitExecutionFixture([4]float64{100, 114, 99, 113}, [4]float64{85, 90, 84, 88})
	o, e := ReplayExitSample(s, m, ExitRunner, 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range o.Ledger {
		if x.Kind == "partial" {
			t.Fatal("partial after stop")
		}
	}
	nearExit(t, o.Gross, -30)
	s, m = exitExecutionFixture([4]float64{100, 114, 99, 113}, [4]float64{111, 112, 110, 111})
	o, e = ReplayExitSample(s, m, ExitRunner, 1)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, x := range o.Ledger {
		if x.Kind == "partial" {
			found = true
			nearExit(t, x.Price, 111)
		}
	}
	if !found {
		t.Fatal("no partial")
	}
}
func TestExitExecutionFundingSizes(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		s, m := exitExecutionFixture([4]float64{100, 101, 99, 100}, [4]float64{100, 101, 99, 100})
		s.Side = side
		if side == "short" {
			s.InitialStop = 110
			s.InitialTarget = 85
		}
		s.Funding = []Funding{{At: s.EntryAt, Symbol: s.Symbol, Mark: 120, Rate: .001}, {At: s.EntryAt.Add(time.Minute), Symbol: s.Symbol, Mark: 120, Rate: .001}}
		o, e := ReplayExitSample(s, m, ExitFixed, 1)
		if e != nil {
			t.Fatal(e)
		}
		want := -.24
		if side == "short" {
			want = .24
		}
		nearExit(t, o.Funding, want)
	}
	s, m := exitExecutionFixture([4]float64{100, 114, 99, 113}, [4]float64{113, 114, 112, 113}, [4]float64{100, 101, 99, 100})
	s.Funding = []Funding{{At: s.EntryAt.Add(time.Minute), Symbol: s.Symbol, Mark: 120, Rate: .001}, {At: s.EntryAt.Add(2 * time.Minute), Symbol: s.Symbol, Mark: 120, Rate: .001}}
	o, e := ReplayExitSample(s, m, ExitRunner, 1)
	if e != nil {
		t.Fatal(e)
	}
	nearExit(t, o.Funding, -.36)
	s, m = exitExecutionFixture([4]float64{100, 116, 89, 100})
	s.Funding = []Funding{{At: s.EntryAt.Add(30 * time.Second), Symbol: s.Symbol, Mark: 100, Rate: .001}}
	o, e = ReplayExitSample(s, m, ExitFixed, 1)
	if e != nil || !strings.Contains(strings.Join(o.Warnings, " "), "intrabar_funding_ambiguous") {
		t.Fatalf("%+v %v", o, e)
	}
}
func TestExitExecutionCensoring(t *testing.T) {
	s, m := exitExecutionFixture([4]float64{100, 114, 99, 113})
	o, e := ReplayExitSample(s, m, ExitRunner, 1)
	if e != nil {
		t.Fatal(e)
	}
	if o.Status != "censored" || o.CloseAt != nil || o.Remaining != 2 || o.Gross != 0 || o.Fees != 0 {
		t.Fatal(o)
	}
	nearExit(t, o.MarkToMarket, 26)
}
func TestExitExecutionStressAndFuture(t *testing.T) {
	s, m := exitExecutionFixture([4]float64{100, 116, 99, 115})
	s.EntryFee = -.1
	m.Costs.ExitFeeBPS = 5
	m.Costs.SlippageBPS = 3
	s.SpreadBPS = nil
	s.AssumedSpreadBPS = 2
	a, e := ReplayExitSample(s, m, ExitFixed, 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := ReplayExitSample(s, m, ExitFixed, 2)
	if e != nil {
		t.Fatal(e)
	}
	nearExit(t, a.Ledger[1].Price, 114.954)
	nearExit(t, b.Ledger[1].Price, 114.9195)
	if !(b.Net < a.Net) {
		t.Fatal("stress")
	}
	future := s.Bars[0]
	future.Start = s.EndAt
	future.End = s.EndAt.Add(time.Minute)
	future.High = 10000
	s.Bars = append(s.Bars, future)
	again, e := ReplayExitSample(s, m, ExitFixed, 1)
	if e != nil || !reflect.DeepEqual(a, again) {
		t.Fatal("future leakage", e)
	}
	s.EntryFee = .01
	b, e = ReplayExitSample(s, m, ExitFixed, 2)
	if e != nil {
		t.Fatal(e)
	}
	nearExit(t, b.Ledger[0].Fee, .01)
}
