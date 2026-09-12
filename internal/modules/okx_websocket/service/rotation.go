package service

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/models"
)

// ConfigureRotation must run during dependency construction, before Start.
func (s *Service) ConfigureRotation(ready func() bool, prepare func(context.Context, []string) error) {
	s.rotationReady = ready
	s.prepareRotation = prepare
}
func (s *Service) Selected(symbol string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, v := range s.watch {
		if v == symbol {
			return true
		}
	}
	return false
}
func (s *Service) EntryAllowed(symbol string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	selected := false
	for _, v := range s.watch {
		if v == symbol {
			selected = true
			break
		}
	}
	if !selected {
		return false
	}
	now := time.Now()
	for _, tf := range uniqTimeframes("1m", s.cfg.Strategy.LTF, s.cfg.Strategy.HTF) {
		at := s.instrumentLastSeen[symbol][toOKXBar(tf)]
		if at.IsZero() || at.After(now) || now.Sub(at) > 90*time.Second {
			return false
		}
	}
	return true
}
func (s *Service) runRotation(ctx context.Context, initial []string, out chan<- models.CandleTick) {
	interval := s.cfg.Strategy.Universe.RefreshInterval
	s.mu.Lock()
	s.retainedCount = len(initial)
	s.mu.Unlock()
	if interval == 0 {
		return
	}
	if interval < 15*time.Minute {
		interval = 15 * time.Minute
	}
	if s.rotationReady == nil || s.prepareRotation == nil {
		return
	}
	retained := map[string]bool{}
	for _, sym := range initial {
		retained[sym] = true
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.rotationReady() {
				continue
			}
			err := s.rotate(ctx, retained, out)
			s.mu.Lock()
			s.rotationAt = time.Now()
			s.rotationError = ""
			if err != nil {
				s.rotationError = "Обновление списка не выполнено; сохранён предыдущий список"
			}
			s.retainedCount = len(retained)
			s.mu.Unlock()
		}
	}
}
func (s *Service) rotate(ctx context.Context, retained map[string]bool, out chan<- models.CandleTick) error {
	r, err := s.rankedUniverse(ctx, s.cfg.Strategy.WatchTopN, models.UniverseConservative)
	if err != nil {
		return err
	}
	if len(r.Selected) == 0 {
		return fmt.Errorf("empty candidate universe")
	}
	next := []string{}
	prepare := []string{}
	subscribe := []string{}
	for _, v := range r.Selected {
		next = append(next, v.Symbol)
		if !s.Selected(v.Symbol) {
			prepare = append(prepare, v.Symbol)
		}
		if !retained[v.Symbol] {
			subscribe = append(subscribe, v.Symbol)
		}
	}
	// Bounded retention deliberately keeps market streams for removed instruments:
	// any existing positions continue receiving 1m trailing data. No forced exits.
	if len(retained)+len(subscribe) > 500 {
		return fmt.Errorf("retained stream safety cap reached")
	}
	warmCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := s.prepareRotation(warmCtx, prepare); err != nil {
		return err
	}
	for _, sym := range subscribe {
		retained[sym] = true
	}
	if len(subscribe) > 0 {
		for _, tf := range uniqTimeframes("1m", s.cfg.Strategy.LTF, s.cfg.Strategy.HTF) {
			go s.runTimeframe(ctx, toOKXBar(tf), subscribe, out)
		}
	}
	// Admission remains disabled for a newly selected symbol until all its live
	// streams are fresh. Historical seed never enables a live order by itself.
	s.mu.Lock()
	defer s.mu.Unlock()
	held := map[string]time.Time{}
	now := time.Now()
	for _, sym := range next {
		held[sym] = s.selectedSince[sym]
		if held[sym].IsZero() {
			held[sym] = now
		}
	}
	s.watch = next
	s.selectedSince = held
	return nil
}
