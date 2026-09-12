package research

import (
	"reflect"
	"testing"
	"time"
)

func TestBootstrapIsDeterministicAndRejectsTinySamples(t *testing.T) {
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(40 * 24 * time.Hour)
	if dailyInterval(nil, start, end) != nil {
		t.Fatal("invented confidence")
	}
	ts := make([]Trade, 120)
	for i := range ts {
		ts[i] = Trade{CloseAt: start.Add(time.Duration(i%40) * 24 * time.Hour), Net: 1}
	}
	a, b := dailyInterval(ts, start, end), dailyInterval(ts, start, end)
	if !reflect.DeepEqual(a, b) || a.Lower != 3 || a.Upper != 3 {
		t.Fatalf("%+v %+v", a, b)
	}
}
