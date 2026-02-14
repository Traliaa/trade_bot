package service

import (
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
