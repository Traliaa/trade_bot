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
	RejectWeakClose     RejectReason = "weak_close" // weak_close_up + weak_close_down (aggregated)
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
// Snapshot возвращает снимок. Если reset=true — обнуляет счётчики и начинает новый период.
func (s *RejectStats) Snapshot(reset bool) RejectSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 1) копия map (и сразу же соберём агрегат weak_close)
	byCopy := make(map[RejectReason]uint64, len(s.by)+1)

	var weakUp, weakDn uint64
	for r, c := range s.by {
		byCopy[r] = c
		if r == RejectWeakCloseUp {
			weakUp = c
		}
		if r == RejectWeakCloseDown {
			weakDn = c
		}
	}

	weakTotal := weakUp + weakDn
	if weakTotal > 0 {
		// добавляем агрегат
		byCopy[RejectWeakClose] = weakTotal

		// (опционально, но я бы так и сделал)
		// чтобы в UI и в любом месте не всплывали up/down отдельно:
		delete(byCopy, RejectWeakCloseUp)
		delete(byCopy, RejectWeakCloseDown)
	}

	// 2) top из byCopy (уже “нормализованного”)
	top := make([]RejectTopItem, 0, len(byCopy))
	for r, c := range byCopy {
		top = append(top, RejectTopItem{Reason: r, Count: c})
	}

	sort.Slice(top, func(i, j int) bool {
		if top[i].Count == top[j].Count {
			return top[i].Reason < top[j].Reason // стабильность для одинаковых значений
		}
		return top[i].Count > top[j].Count
	})

	if len(top) > 10 {
		top = top[:10]
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
func (s *RejectStats) Touch(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.from.IsZero() {
		s.from = now
	}
	// даже без Inc() окно будет “течь”
	s.to = now
}
