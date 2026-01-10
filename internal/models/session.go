package models

import (
	"time"
)

type PosKey struct {
	InstID  string
	PosSide string // "long"/"short"
}

type CachedPos struct {
	InstID    string
	PosSide   string
	Size      float64
	Entry     float64
	LastPx    float64
	UpdatedAt time.Time
}

type OpenResult struct {
	PosSide  string  // "long"/"short"
	TPAlgoID string  // TP algoId
	SLAlgoID string  // SL algoId
	Entry    float64 // если уточнил, иначе params.Entry
}
