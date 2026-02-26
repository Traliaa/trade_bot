package models

import (
	"sort"
	"sync"
	"time"
)

type RejectReason string

const (
	RejectInvalidPrice  RejectReason = "invalid_price"
	RejectNoChannel     RejectReason = "no_channel"
	RejectNotReady      RejectReason = "not_ready"
	RejectNoTrend       RejectReason = "no_trend"
	RejectSmallChannel  RejectReason = "small_channel"
	RejectSmallBody     RejectReason = "small_body"
	RejectWeakCloseUp   RejectReason = "weak_close_up"
	RejectWeakCloseDown RejectReason = "weak_close_down"
	RejectNoBreakout    RejectReason = "no_breakout"
	RejectZeroRange     RejectReason = "zero_range"
	RejectCooldown      RejectReason = "cooldown"
	RejectInternal      RejectReason = "internal"
)

type RejectTopItem struct {
	Reason RejectReason
	Count  uint64
}

type RejectSnapshot struct {
	From  time.Time
	To    time.Time
	Total uint64
	Top   []RejectTopItem // отсортировано по убыванию
	By    map[RejectReason]uint64
}

type RejectStats struct {
	mu            sync.Mutex
	from          time.Time
	to            time.Time
	total         uint64
	by            map[RejectReason]uint64
	LastRejectLog time.Time
}

func NewRejectStats() *RejectStats {
	now := time.Now()
	return &RejectStats{
		from: now,
		to:   now,
		by:   make(map[RejectReason]uint64),
	}
}

func (s *RejectStats) Inc(r RejectReason) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.from.IsZero() {
		s.from = now
	}
	s.to = now

	s.total++
	s.by[r]++
}

// Snapshot возвращает снимок. Если reset=true — обнуляет счётчики и начинает новый период.
func (s *RejectStats) Snapshot(reset bool) RejectSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	// top
	top := make([]RejectTopItem, 0, len(s.by))
	for r, c := range s.by {
		top = append(top, RejectTopItem{Reason: r, Count: c})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Count > top[j].Count })
	if len(top) > 10 {
		top = top[:10]
	}

	byCopy := make(map[RejectReason]uint64, len(s.by))
	for r, c := range s.by {
		byCopy[r] = c
	}

	out := RejectSnapshot{
		From:  s.from,
		To:    s.to,
		Total: s.total,
		Top:   top,
		By:    byCopy,
	}

	if reset {
		now := time.Now()
		s.from = now
		s.to = now
		s.total = 0
		s.by = make(map[RejectReason]uint64)
	}

	return out
}
