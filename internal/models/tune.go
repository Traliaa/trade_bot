package models

import "time"

type RuntimeTuning struct {
	MinChannelPct float64
	MinBodyPct    float64
	BreakoutPct   float64
	CloseUpMin    float64 // было 0.80
	CloseDnMax    float64 // было 0.20
}

type TuneMode int

const (
	TuneOff  TuneMode = iota
	TuneSafe          // мягко и ограниченно
	TuneAuto          // агрессивнее
)

type TuneWhy string

const (
	TuneWhyOK            TuneWhy = "ok"
	TuneWhyOff           TuneWhy = "off"
	TuneWhyWarmup        TuneWhy = "warmup_not_done"
	TuneWhyCooldown      TuneWhy = "cooldown"
	TuneWhySignalsRecent TuneWhy = "signals_recent"
	TuneWhyNotEnoughData TuneWhy = "not_enough_rejects"
	TuneWhyNoDominant    TuneWhy = "no_dominant_reason"
	TuneWhyUnknown       TuneWhy = "unknown"
	TuneWhyNoSignalsYet  TuneWhy = "no_signals_yet"
)

type TuneDecision struct {
	Changed  bool
	Why      TuneWhy
	Before   RuntimeTuning
	After    RuntimeTuning
	Dominant RejectReason
	DomPct   float64
	Total    uint64

	// для удобной админки/логов
	From time.Time
	To   time.Time
}

func TuneModeString(m TuneMode) string {
	switch m {
	case TuneOff:
		return "OFF"
	case TuneSafe:
		return "SAFE"
	case TuneAuto:
		return "AUTO"
	default:
		return "UNKNOWN"
	}
}
