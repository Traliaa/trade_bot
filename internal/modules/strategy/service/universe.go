package service

import (
	"sort"
	"time"
	"trade_bot/internal/models"
)

// Set once before lifecycle Start; callbacks contain their own synchronization.
func (e *Service) ConfigureUniverse(selected, allowed func(string) bool) {
	e.selected = selected
	e.entryAllowed = allowed
}
func (e *Service) EntryAllowed(symbol string) bool {
	return e.entryAllowed == nil || e.entryAllowed(symbol)
}

// SeedV3 imports closed history without scoring or emitting signals.
func (e *Service) SeedV3(symbol string, ltf, htf []models.CandleTick) {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.getV3MarketStateLocked(symbol)
	m.LTFCandles = mergeSeed(m.LTFCandles, ltf)
	m.HTFCandles = mergeSeed(m.HTFCandles, htf)
}
func mergeSeed(live, history []models.CandleTick) []models.CandleTick {
	byEnd := map[time.Time]models.CandleTick{}
	for _, c := range history {
		byEnd[c.End] = c
	}
	for _, c := range live {
		byEnd[c.End] = c
	}
	out := make([]models.CandleTick, 0, len(byEnd))
	for _, c := range byEnd {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].End.Before(out[j].End) })
	if len(out) > 250 {
		out = out[len(out)-250:]
	}
	return out
}
