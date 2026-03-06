package models

import "time"

type PendingEntry struct {
	Active bool
	Side   Side

	Level float64

	Created  time.Time
	ExpireAt time.Time

	BreakCandleEnd time.Time
	BreakClosePos  float64
	BreakBodyPct   float64
	BreakRangePct  float64

	Touched        bool
	Confirmed      bool
	DeepRetestSeen bool
}
