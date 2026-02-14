package service

import (
	"maps"
	"sort"
	"sync"
	"time"
)

type RejectReason string

const (
	RejectNotReady      RejectReason = "not_ready"      // индикаторы не готовы / прогрев не завершён
	RejectNoTrend       RejectReason = "no_trend"       // не смогли определить тренд на HTF
	RejectTrendMismatch RejectReason = "trend_mismatch" // тренд против сигнала
	RejectSmallChannel  RejectReason = "small_channel"  // канал узкий (MinChannelPct)
	RejectSmallBody     RejectReason = "small_body"     // свеча слишком маленькая (MinBodyPct)
	RejectNoBreakout    RejectReason = "no_breakout"    // нет пробоя (BreakoutPct)
	RejectCooldown      RejectReason = "cooldown"       // защита от частых сигналов
	RejectInternal      RejectReason = "internal"       // прочее/ошибка
)

type RejectStats struct {
	mu     sync.Mutex
	total  uint64
	by     map[RejectReason]uint64
	lastAt time.Time
}

func NewRejectStats() *RejectStats {
	return &RejectStats{
		by: make(map[RejectReason]uint64),
	}
}

type RejectSnapshot struct {
	From  time.Time
	To    time.Time
	Total int
	By    map[string]int
	Top   []RejectTopItem
}

type RejectTopItem struct {
	Reason string
	Count  int
}

func (e *DonchianV2HTF) RejectSnapshot(reset bool) RejectSnapshot {
	e.rejectMu.Lock()
	defer e.rejectMu.Unlock()

	now := time.Now()

	// если первый раз
	if e.lastLog.IsZero() {
		e.lastLog = now
	}

	total := 0
	for _, v := range e.rejectStats {
		total += v
	}

	// top (до 10)
	top := make([]RejectTopItem, 0, len(e.rejectStats))
	for k, v := range e.rejectStats {
		top = append(top, RejectTopItem{Reason: k, Count: v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Count > top[j].Count })
	if len(top) > 10 {
		top = top[:10]
	}

	out := RejectSnapshot{
		From:  e.lastLog,
		To:    now,
		Total: total,
		By:    maps.Clone(e.rejectStats),
		Top:   top,
	}

	if reset {
		e.rejectStats = make(map[string]int)
		e.lastLog = now
	}

	return out
}

func (s *RejectStats) Inc(r RejectReason) {
	s.mu.Lock()
	s.total++
	s.by[r]++
	s.lastAt = time.Now()
	s.mu.Unlock()
}

func (s *RejectStats) SnapshotAndReset() (total uint64, lastAt time.Time, top []struct {
	Reason RejectReason
	Count  uint64
}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	type kv struct {
		r RejectReason
		c uint64
	}
	arr := make([]kv, 0, len(s.by))
	for r, c := range s.by {
		arr = append(arr, kv{r: r, c: c})
	}
	sort.Slice(arr, func(i, j int) bool { return arr[i].c > arr[j].c })

	out := make([]struct {
		Reason RejectReason
		Count  uint64
	}, 0, len(arr))

	for _, it := range arr {
		out = append(out, struct {
			Reason RejectReason
			Count  uint64
		}{Reason: it.r, Count: it.c})
	}

	total = s.total
	lastAt = s.lastAt

	// reset
	s.total = 0
	s.by = make(map[RejectReason]uint64)

	return total, lastAt, out
}
