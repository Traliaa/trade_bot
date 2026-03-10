package models

import (
	"github.com/google/uuid"
	"time"
)

type TradeStatus string

const (
	TradeStatusOpen    TradeStatus = "open"
	TradeStatusClosed  TradeStatus = "closed"
	TradeStatusPartial TradeStatus = "partial"
)

type CloseReason string

const (
	CloseReasonTP       CloseReason = "tp"
	CloseReasonSL       CloseReason = "sl"
	CloseReasonTimeStop CloseReason = "time_stop"
	CloseReasonManual   CloseReason = "manual"
	CloseReasonUnknown  CloseReason = "unknown"
)

type TradeRecord struct {
	GUID      uuid.UUID
	UserID    int64
	InstID    string
	PosSide   string // long/short
	Side      string // buy/sell
	Timeframe string
	Strategy  string

	EntryPrice float64
	EntrySize  float64
	EntryAt    time.Time

	StopLoss   float64
	TakeProfit float64
	Leverage   int

	OpenOrderID string
	AlgoID      string

	ExitPrice      float64
	ExitSize       float64
	ExitAt         *time.Time
	RealizedPnL    float64
	RealizedPnLPct float64
	CloseReason    CloseReason

	Status    TradeStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

type TradeCloseInput struct {
	ExitPrice      float64
	ExitSize       float64
	ExitAt         time.Time
	RealizedPnL    float64
	RealizedPnLPct float64
	CloseReason    CloseReason
}
