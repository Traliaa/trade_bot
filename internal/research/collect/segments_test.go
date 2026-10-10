package collect

import (
	"reflect"
	"testing"
	"time"
)

func TestSegmentsExact72Hours(t *testing.T) {
	base := time.Date(2026, 10, 8, 10, 0, 0, 0, time.UTC)
	for _, delta := range []time.Duration{0, 3 * time.Millisecond, 59*time.Second + 999*time.Millisecond} {
		entry := base.Add(delta)
		segments, err := PlanSegments(entry)
		if err != nil {
			t.Fatal(err)
		}
		at := entry
		full, boundary := 0, 0
		for _, s := range segments {
			if !s.Start.Equal(at) || !s.End.After(s.Start) {
				t.Fatal("gap or overlap")
			}
			at = s.End
			if s.Kind == "minute" {
				full++
				if s.End.Sub(s.Start) != time.Minute {
					t.Fatal("not a full minute")
				}
			} else {
				boundary++
			}
		}
		if !at.Equal(entry.Add(72 * time.Hour)) {
			t.Fatal("rounded horizon")
		}
		if delta == 0 && (full != 4320 || boundary != 0) || delta != 0 && (full != 4319 || boundary != 2) {
			t.Fatalf("full=%d boundary=%d", full, boundary)
		}
	}
	if _, err := PlanSegments(time.Time{}); err == nil {
		t.Fatal("zero time accepted")
	}
}

func TestBoundaryRightExclusive(t *testing.T) {
	entry := time.Date(2026, 10, 8, 10, 0, 2, 0, time.UTC)
	segments, _ := PlanSegments(entry)
	s := segments[0]
	ticks := []Tick{{"public-0", entry.Add(-time.Millisecond), "1"}, {"public-1", entry, "100"}, {"public-2", entry.Add(time.Second), "103"}, {"public-3", s.End.Add(-time.Millisecond), "101"}, {"public-4", s.End, "1000"}}
	r, err := BoundaryOHLC(s, ticks, true)
	if err != nil || r.Coverage != "complete" || r.Trades != 3 || r.Prices == nil || *r.Prices != (OHLC{"100", "103", "100", "101"}) {
		t.Fatalf("report=%+v err=%v", r, err)
	}
	ticks = append(ticks, ticks[1])
	r2, err := BoundaryOHLC(s, ticks, true)
	if err != nil || !reflect.DeepEqual(r, r2) {
		t.Fatal("duplicate changed boundary")
	}
	ticks[len(ticks)-1].Price = "99"
	if _, err := BoundaryOHLC(s, ticks, true); err == nil {
		t.Fatal("conflicting public duplicate accepted")
	}
}

func TestBoundaryEmptyIncomplete(t *testing.T) {
	s := Segment{Kind: "boundary", Start: time.Date(2026, 10, 8, 10, 0, 2, 0, time.UTC), End: time.Date(2026, 10, 8, 10, 1, 0, 0, time.UTC)}
	r, err := BoundaryOHLC(s, nil, false)
	if err != nil || r.Coverage != "incomplete" || r.Prices != nil {
		t.Fatal("empty incomplete invented prices")
	}
	r, err = BoundaryOHLC(s, nil, true)
	if err != nil || r.Coverage != "complete" || r.Prices != nil {
		t.Fatal("known empty confused with missing")
	}
	r, err = BoundaryOHLC(s, []Tick{{"1", s.Start, "100"}, {"2", s.Start, "101"}}, true)
	if err != nil || r.Coverage != "incomplete" || r.Prices != nil {
		t.Fatal("invented order within same timestamp")
	}
}
