package healthwatch

import "time"

// State is persisted by the independent monitor, not by the trading process.
type State struct {
	Started          time.Time `json:"started"`
	Failures         int       `json:"failures"`
	Successes        int       `json:"successes"`
	Incident         bool      `json:"incident"`
	NotifiedIncident bool      `json:"notified_incident"`
}

func (s *State) Observe(now time.Time, healthy bool) string {
	if s.Started.IsZero() {
		s.Started = now
	}
	if healthy {
		s.Failures = 0
		s.Successes++
		if s.Successes >= 2 {
			s.Incident = false
		}
	} else {
		s.Successes = 0
		s.Failures++
		if s.Failures >= 3 && now.Sub(s.Started) >= 5*time.Minute {
			s.Incident = true
		}
	}
	if s.Incident != s.NotifiedIncident {
		if s.Incident {
			return "unavailable"
		}
		return "recovered"
	}
	return ""
}
func (s *State) Acknowledge() { s.NotifiedIncident = s.Incident }
