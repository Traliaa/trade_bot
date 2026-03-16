package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type TradeStatus string

const (
	TradeStatusOpen   TradeStatus = "open"
	TradeStatusClosed TradeStatus = "closed"
)

type CloseReason string

const (
	CloseReasonUnknown       CloseReason = "unknown"
	CloseReasonTP            CloseReason = "tp"
	CloseReasonSL            CloseReason = "sl"
	CloseReasonBreakEven     CloseReason = "break_even"
	CloseReasonLockProfit    CloseReason = "lock_profit"
	CloseReasonPartialExit   CloseReason = "partial_exit"
	CloseReasonTimeStopEarly CloseReason = "time_stop_early"
	CloseReasonTimeStopStale CloseReason = "time_stop_stale"
	CloseReasonManual        CloseReason = "manual"
	CloseReasonRecovery      CloseReason = "recovery"
	CloseReasonForceClose    CloseReason = "force_close"
)

func NormalizeCloseReason(v string) CloseReason {
	switch CloseReason(v) {
	case CloseReasonTP,
		CloseReasonSL,
		CloseReasonBreakEven,
		CloseReasonLockProfit,
		CloseReasonPartialExit,
		CloseReasonTimeStopEarly,
		CloseReasonTimeStopStale,
		CloseReasonManual,
		CloseReasonRecovery,
		CloseReasonForceClose:
		return CloseReason(v)
	default:
		return CloseReasonUnknown
	}
}

type TradePayload struct {
	PosSide string `json:"pos_side"`
	Side    string `json:"side"`

	EntryPrice float64 `json:"entry_price"`
	EntrySize  float64 `json:"entry_size"`
	StopLoss   float64 `json:"stop_loss"`
	TakeProfit float64 `json:"take_profit"`
	Leverage   int64   `json:"leverage"`

	OpenOrderID string `json:"open_order_id,omitempty"`
	AlgoID      string `json:"algo_id,omitempty"`

	ExitPrice float64 `json:"exit_price,omitempty"`
	ExitSize  float64 `json:"exit_size,omitempty"`

	RealizedPnL    float64 `json:"realized_pnl,omitempty"`
	RealizedPnLPct float64 `json:"realized_pnl_pct,omitempty"`

	RiskDist    float64 `json:"risk_dist,omitempty"`
	RMultiple   float64 `json:"r_multiple,omitempty"`
	DurationSec int64   `json:"duration_sec,omitempty"`

	MovedToBE         bool `json:"moved_to_be,omitempty"`
	LockedProfit      bool `json:"locked_profit,omitempty"`
	TookPartial       bool `json:"took_partial,omitempty"`
	PartialCount      int  `json:"partial_count,omitempty"`
	TimeStopTriggered bool `json:"time_stop_triggered,omitempty"`

	MFEPrice float64 `json:"mfe_price,omitempty"`
	MAEPrice float64 `json:"mae_price,omitempty"`
	MFER     float64 `json:"mfe_r,omitempty"`
	MAER     float64 `json:"mae_r,omitempty"`
}

type TradeRecord struct {
	GUID        uuid.UUID    `json:"guid"`
	UserID      int64        `json:"user_id"`
	InstID      string       `json:"inst_id"`
	Strategy    string       `json:"strategy"`
	Timeframe   string       `json:"timeframe"`
	Status      TradeStatus  `json:"status"`
	CloseReason CloseReason  `json:"close_reason"`
	EntryAt     time.Time    `json:"entry_at"`
	ExitAt      *time.Time   `json:"exit_at,omitempty"`
	Payload     TradePayload `json:"payload"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

func (p TradePayload) Marshal() ([]byte, error) {
	return json.Marshal(p)
}

func UnmarshalTradePayload(raw []byte) (TradePayload, error) {
	if len(raw) == 0 {
		return TradePayload{}, nil
	}

	var p TradePayload
	err := json.Unmarshal(raw, &p)
	return p, err
}

func NewOpenTrade(
	userID int64,
	instID string,
	strategy string,
	timeframe string,
	entryAt time.Time,
	payload TradePayload,
) TradeRecord {
	return TradeRecord{
		GUID:        uuid.New(),
		UserID:      userID,
		InstID:      instID,
		Strategy:    strategy,
		Timeframe:   timeframe,
		Status:      TradeStatusOpen,
		CloseReason: CloseReasonUnknown,
		EntryAt:     entryAt,
		Payload:     payload,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

type TradeCloseInput struct {
	ExitAt      time.Time
	CloseReason CloseReason
	Payload     TradePayload
}
