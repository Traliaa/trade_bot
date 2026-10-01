package research

import (
	"math"
	"testing"
	"time"
)

func exitTestBar(at time.Time, price float64) ExitBar {
	return ExitBar{Start: at.Add(-time.Minute), End: at, Open: price, High: price, Low: price, Close: price}
}
func exitTestState(t *testing.T, p ExitProfile) (ExitState, ExitPolicyConfig) {
	t.Helper()
	s := ExampleExitDataset().Samples[0]
	st, e := NewExitState(s, p)
	if e != nil {
		t.Fatal(e)
	}
	return st, s.Config
}
func TestExitPolicyRunnerThreshold(t *testing.T) {
	for _, tt := range []struct {
		side              string
		stop, price, want float64
	}{{"long", 90, 130, 120}, {"short", 110, 70, 80}} {
		s := ExampleExitDataset().Samples[0]
		s.Side = tt.side
		s.InitialStop = tt.stop
		if tt.side == "short" {
			s.InitialTarget = 85
		}
		st, e := NewExitState(s, ExitRunner)
		if e != nil {
			t.Fatal(e)
		}
		c := s.Config
		c.PartialEnabled = false
		st, d, e := DecideExit(st, c, exitTestBar(s.EntryAt.Add(time.Minute), tt.price))
		if e != nil || !st.RunnerActive || d.Kind != "move_stop" || d.NewStop != tt.want {
			t.Fatalf("%+v %+v %v", st, d, e)
		}
	}
	st, c := exitTestState(t, ExitRunner)
	c.PartialEnabled = false
	st, _, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), 129.99))
	if st.RunnerActive {
		t.Fatal("early activation")
	}
}
func TestExitPolicyNeverLoosensAndRounds(t *testing.T) {
	for _, tt := range []struct {
		side                string
		old, px, tick, want float64
	}{{"long", 125, 132, .1, 125}, {"short", 75, 68, .1, 75}, {"long", 90, 130.1, .3, 120}, {"short", 110, 69.9, .3, 80.1}} {
		st, c := exitTestState(t, ExitRunner)
		st.Side = tt.side
		st.RunnerActive = true
		st.PartialDone = true
		st.Stop = tt.old
		st.TickSize = tt.tick
		c.PartialEnabled = false
		after, d, e := DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), tt.px))
		got := st.Stop
		if d.Kind == "move_stop" {
			got = d.NewStop
		}
		if e != nil || math.Abs(got-tt.want) > 1e-9 || after.InitialRiskDist != 10 {
			t.Fatalf("%+v %v", d, e)
		}
	}
}
func TestExitPolicyPartialMinimum(t *testing.T) {
	st, c := exitTestState(t, ExitConfigured)
	st.Remaining = 1
	_, d, e := DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), 113))
	if e != nil || d.Kind != "move_stop" || d.NewStop != 101 {
		t.Fatalf("%+v %v", d, e)
	}
	st.Remaining = 2
	_, d, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), 113))
	if d.Kind != "partial" || d.Size != 1 {
		t.Fatal(d)
	}
	st.PartialDone = true
	_, d, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), 113))
	if d.Kind == "partial" {
		t.Fatal("repeat")
	}
}
func TestExitPolicyConfiguredPriority(t *testing.T) {
	st, c := exitTestState(t, ExitConfigured)
	st.MFEPrice = 114
	_, d, _ := DecideExit(st, c, exitTestBar(st.EntryAt.Add(180*time.Minute), 99))
	if d.Kind != "close" || d.Reason != "time_stop" {
		t.Fatal(d)
	}
	st.MFEPrice = 100
	_, d, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(179*time.Minute), 99))
	if d.Kind == "close" {
		t.Fatal(d)
	}
	_, d, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(180*time.Minute), 100))
	if d.Kind == "close" {
		t.Fatal(d)
	}
	st, c = exitTestState(t, ExitConfigured)
	st.BEActivated = true
	st.PartialDone = true
	st.Stop = 105.5
	_, d, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), 113))
	if d.Kind != "none" {
		t.Fatal("lock under 0.1R", d)
	}
}
func TestExitPolicySlotsAndState(t *testing.T) {
	st, c := exitTestState(t, ExitConfigured)
	at := st.EntryAt.Add(time.Minute)
	st.LastActionSlot = st.EntryAt
	_, d, _ := DecideExit(st, c, exitTestBar(at, 113))
	if d.Kind != "none" {
		t.Fatal(d)
	}
	_, d, _ = DecideExit(st, c, exitTestBar(st.EntryAt.Add(15*time.Minute), 113))
	if d.Kind != "partial" {
		t.Fatal(d)
	}
	st.Profile = ExitRunner
	st.LastRunnerActionAt = at
	_, d, _ = DecideExit(st, c, exitTestBar(at, 130))
	if d.Kind != "none" {
		t.Fatal("minute repeat", d)
	}
	_, d, _ = DecideExit(st, c, exitTestBar(at.Add(time.Minute), 130))
	if d.Kind == "none" {
		t.Fatal("runner blocked by slot")
	}
	st, c = exitTestState(t, ExitConfigured)
	c.TimeStopBars = 0
	at = st.EntryAt.Add(240 * time.Minute)
	st, d, _ = DecideExit(st, c, exitTestBar(at, 99))
	if !st.IsStale || d.Kind != "none" {
		t.Fatalf("%+v %+v", st, d)
	}
	_, d, _ = DecideExit(st, c, exitTestBar(at.Add(90*time.Minute), 93))
	if d.Kind != "close" {
		t.Fatal(d)
	}
}
func TestExitPolicyProfileSeparation(t *testing.T) {
	st, c := exitTestState(t, ExitFixed)
	_, d, _ := DecideExit(st, c, exitTestBar(st.EntryAt.Add(time.Minute), 114))
	if d.Kind != "none" || st.Target != 115 {
		t.Fatal(d)
	}
	r, _ := exitTestState(t, ExitRunner)
	if r.Target != 0 {
		t.Fatal("runner TP")
	}
	r.RunnerActive = true
	r.PartialDone = true
	_, d, _ = DecideExit(r, c, exitTestBar(r.EntryAt.Add(500*time.Minute), 105))
	if d.Kind == "close" {
		t.Fatal("runner timed out")
	}
}
func TestExitPolicyMFEVsClose(t *testing.T) {
	st, c := exitTestState(t, ExitRunner)
	b := exitTestBar(st.EntryAt.Add(time.Minute), 110)
	b.High = 131
	after, d, e := DecideExit(st, c, b)
	if e != nil || after.RunnerActive || d.Kind != "partial" {
		t.Fatalf("%+v %+v %v", after, d, e)
	}
}
