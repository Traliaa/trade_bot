package models

import (
	"sort"
	"sync"
	"time"
)

type RejectReason string

const (
	RejectInvalidPrice  RejectReason = "invalid_price"
	RejectNotReady      RejectReason = "not_ready"
	RejectNoTrend       RejectReason = "no_trend"
	RejectSmallChannel  RejectReason = "small_channel"
	RejectSmallBody     RejectReason = "small_body"
	RejectZeroRange     RejectReason = "zero_range"
	RejectNoBreakout    RejectReason = "no_breakout"
	RejectWeakClose     RejectReason = "weak_close"
	RejectWeakCloseUp   RejectReason = "weak_close_up"
	RejectWeakCloseDown RejectReason = "weak_close_down"
	RejectInternal      RejectReason = "internal"

	RejectPendingCooldown         RejectReason = "pending_cooldown"
	RejectPendingAdverseMove      RejectReason = "pending_adverse_move"
	RejectPendingExpiredNoTouch   RejectReason = "pending_expired_no_touch"
	RejectPendingExpiredNoConfirm RejectReason = "pending_expired_no_confirm"
	RejectPendingShallowRetest    RejectReason = "pending_shallow_retest"
	RejectBreakoutTooLong         RejectReason = "breakout_too_long"
	RejectPendingBadConfirmCandle RejectReason = "pending_bad_confirm_candle"
	RejectStaleRetest             RejectReason = "stale_retest"
	RejectLateRetestStretch       RejectReason = "late_retest_stretch"
)

type RejectTopItem struct {
	Reason RejectReason
	Count  uint64
}

type RejectSnapshot struct {
	From time.Time
	To   time.Time

	Total  uint64
	Top    []RejectTopItem
	Counts map[RejectReason]uint64

	AvgCloseUp   float64
	AvgCloseDown float64
}
type RejectStats struct {
	mu sync.Mutex

	from time.Time
	to   time.Time

	counts map[RejectReason]uint64
	total  uint64

	weakCloseUpSum   float64
	weakCloseUpCount uint64

	weakCloseDnSum   float64
	weakCloseDnCount uint64
}

func NewRejectStats() *RejectStats {
	now := time.Now()
	return &RejectStats{
		from:   now,
		to:     now,
		counts: make(map[RejectReason]uint64),
	}
}

func (r *RejectStats) Inc(reason RejectReason) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if r.total == 0 && r.from.IsZero() {
		r.from = now
	}
	r.to = now
	r.counts[reason]++
	r.total++
}

func (r *RejectStats) IncWeakClose(reason RejectReason, closePos float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if r.total == 0 && r.from.IsZero() {
		r.from = now
	}
	r.to = now

	r.counts[reason]++
	r.counts[RejectWeakClose]++
	r.total++

	switch reason {
	case RejectWeakCloseUp:
		r.weakCloseUpSum += closePos
		r.weakCloseUpCount++
	case RejectWeakCloseDown:
		r.weakCloseDnSum += closePos
		r.weakCloseDnCount++
	}
}

func (r *RejectStats) Snapshot(reset bool) RejectSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	snap := RejectSnapshot{
		From:   r.from,
		To:     r.to,
		Total:  r.total,
		Counts: make(map[RejectReason]uint64, len(r.counts)),
	}

	for k, v := range r.counts {
		snap.Counts[k] = v
		snap.Top = append(snap.Top, RejectTopItem{
			Reason: k,
			Count:  v,
		})
	}

	sort.Slice(snap.Top, func(i, j int) bool {
		if snap.Top[i].Count == snap.Top[j].Count {
			return snap.Top[i].Reason < snap.Top[j].Reason
		}
		return snap.Top[i].Count > snap.Top[j].Count
	})

	if r.weakCloseUpCount > 0 {
		snap.AvgCloseUp = r.weakCloseUpSum / float64(r.weakCloseUpCount)
	}
	if r.weakCloseDnCount > 0 {
		snap.AvgCloseDown = r.weakCloseDnSum / float64(r.weakCloseDnCount)
	}

	if reset {
		r.from = now
		r.to = now
		r.total = 0
		r.counts = make(map[RejectReason]uint64)

		r.weakCloseUpSum = 0
		r.weakCloseUpCount = 0
		r.weakCloseDnSum = 0
		r.weakCloseDnCount = 0
	}

	return snap
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
