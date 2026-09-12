package service

import (
	"testing"
	"time"
)

func TestRoutineNoticesAreGroupedAndStatsAreCopies(t *testing.T) {
	r := &Service{}
	now := time.Now()
	if ok, _ := r.noticeAllowed("1/limit", now); !ok {
		t.Fatal("first hidden")
	}
	if ok, _ := r.noticeAllowed("1/limit", now.Add(time.Minute)); ok {
		t.Fatal("repeat sent")
	}
	if ok, _ := r.noticeAllowed("2/limit", now); !ok {
		t.Fatal("users mixed")
	}
	if ok, n := r.noticeAllowed("1/limit", now.Add(16*time.Minute)); !ok || n != 1 {
		t.Fatal(ok, n)
	}
	r.countDecision("opened")
	a := r.ExecutionStats()
	a["opened"] = 999
	if r.ExecutionStats()["opened"] != 1 {
		t.Fatal("shared map")
	}
}
