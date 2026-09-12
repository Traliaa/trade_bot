package service

import "time"

func (s *Service) markMarketData(bar string, at time.Time, symbols ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marketLastSeen == nil {
		s.marketLastSeen = make(map[string]time.Time)
	}
	s.marketLastSeen[bar] = at
	for _, symbol := range symbols {
		if symbol == "" {
			continue
		}
		if s.instrumentLastSeen == nil {
			s.instrumentLastSeen = make(map[string]map[string]time.Time)
		}
		if s.instrumentLastSeen[symbol] == nil {
			s.instrumentLastSeen[symbol] = make(map[string]time.Time)
		}
		s.instrumentLastSeen[symbol][bar] = at
	}
}

type UniverseSnapshot struct {
	RefreshAt        time.Time `json:"refresh_at"`
	RefreshError     string    `json:"refresh_error"`
	RetainedStreams  int       `json:"retained_streams"`
	TotalLimit       int       `json:"total_limit"`
	DynamicRequested int       `json:"dynamic_requested"`
	CoreCount        int       `json:"core_count"`
	SelectedCount    int       `json:"selected_count"`
	FreshCount       int       `json:"fresh_count"`
	Symbols          []string  `json:"symbols"`
}

func (s *Service) UniverseStatus(now time.Time) UniverseSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit := s.dynamicRequested
	if s.cfg.Strategy.Universe.TotalLimit != 0 {
		limit = s.cfg.Strategy.Universe.TotalLimit
	}
	snapshot := UniverseSnapshot{TotalLimit: limit, SelectedCount: len(s.watch), Symbols: append([]string{}, s.watch...)}
	snapshot.RefreshAt = s.rotationAt
	snapshot.RefreshError = s.rotationError
	snapshot.RetainedStreams = s.retainedCount
	for _, symbol := range s.watch {
		fresh := true
		for _, tf := range uniqTimeframes("1m", s.cfg.Strategy.LTF, s.cfg.Strategy.HTF) {
			at := s.instrumentLastSeen[symbol][toOKXBar(tf)]
			if at.IsZero() || at.After(now) || now.Sub(at) > 90*time.Second {
				fresh = false
				break
			}
		}
		if fresh {
			snapshot.FreshCount++
		}
	}
	return snapshot
}

// MarketDataUpdatedAt returns the oldest observation across required timeframes.
// Zero means at least one required stream has not supplied data yet.
func (s *Service) MarketDataUpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var oldest time.Time
	for _, tf := range uniqTimeframes("1m", s.cfg.Strategy.LTF, s.cfg.Strategy.HTF) {
		at := s.marketLastSeen[toOKXBar(tf)]
		if at.IsZero() {
			return time.Time{}
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
	}
	return oldest
}
