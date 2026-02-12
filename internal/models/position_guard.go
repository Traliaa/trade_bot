package models

import "time"

type PositionGuardState struct {
	WarnCount   int       `json:"warn_count"`
	LastWarnAt  time.Time `json:"last_warn_at"`
	Blacklisted bool      `json:"blacklisted"`
}

// key = instId + ":" + posSide
type PositionGuardMap map[string]PositionGuardState
