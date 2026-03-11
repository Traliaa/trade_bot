package models

import "time"

type DebugStage string

const (
	DebugStageTrend      DebugStage = "trend"
	DebugStageBreakout   DebugStage = "breakout"
	DebugStagePending    DebugStage = "pending"
	DebugStageRetest     DebugStage = "retest"
	DebugStageDeepRetest DebugStage = "deep_retest"
	DebugStageConfirm    DebugStage = "confirm"
	DebugStageSignal     DebugStage = "signal"
	DebugStageRejected   DebugStage = "rejected"
	DebugStageExpired    DebugStage = "expired"
)

type DebugEvent struct {
	At      time.Time      `json:"at"`
	Stage   DebugStage     `json:"stage"`
	Reason  string         `json:"reason,omitempty"`
	Message string         `json:"message,omitempty"`
	Values  map[string]any `json:"values,omitempty"`
}

type StrategyDebugTrace struct {
	ID        string       `json:"id"`
	InstID    string       `json:"inst_id"`
	Strategy  string       `json:"strategy"`
	Timeframe string       `json:"timeframe"`
	Side      Side         `json:"side"`
	StartedAt time.Time    `json:"started_at"`
	EndedAt   *time.Time   `json:"ended_at,omitempty"`
	Events    []DebugEvent `json:"events"`
}
