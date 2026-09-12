package healthwatch

import (
	"testing"
	"time"
)

func TestQuietStartIncidentRetryAndRecovery(t *testing.T) {
	now := time.Now()
	s := State{Started: now.Add(-time.Hour)}
	if s.Observe(now, true) != "" {
		t.Fatal("startup spam")
	}
	for i := 0; i < 2; i++ {
		if s.Observe(now, false) != "" {
			t.Fatal("premature incident")
		}
	}
	if s.Observe(now, false) != "unavailable" {
		t.Fatal("missing incident")
	}
	if s.Observe(now, false) != "unavailable" {
		t.Fatal("failed delivery not retried")
	}
	s.Acknowledge()
	if s.Observe(now, false) != "" {
		t.Fatal("duplicate")
	}
	if s.Observe(now, true) != "" {
		t.Fatal("premature recovery")
	}
	if s.Observe(now, true) != "recovered" {
		t.Fatal("missing recovery")
	}
	s.Acknowledge()
	if s.Observe(now, true) != "" {
		t.Fatal("repeat recovery")
	}
}
func TestStartupGrace(t *testing.T) {
	s := State{}
	now := time.Now()
	for i := 0; i < 10; i++ {
		if s.Observe(now.Add(time.Duration(i)*time.Second), false) != "" {
			t.Fatal("startup grace ignored")
		}
	}
	if s.Observe(now.Add(5*time.Minute), false) != "unavailable" {
		t.Fatal("never alerted")
	}
}
