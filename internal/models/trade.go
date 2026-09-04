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
	CloseReasonTimeStop      CloseReason = "time_stop"
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
		CloseReasonTimeStop,
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

	EntryPrice  float64 `json:"entry_price"`
	SignalPrice float64 `json:"signal_price,omitempty"`
	EntrySize   float64 `json:"entry_size"`
	StopLoss    float64 `json:"stop_loss"`
	TakeProfit  float64 `json:"take_profit"`
	Leverage    int64   `json:"leverage"`

	OpenOrderID string  `json:"open_order_id,omitempty"`
	AlgoID      string  `json:"algo_id,omitempty"`    // Тут ID Стоп-Лосса
	TPAlgoID    string  `json:"tp_algo_id,omitempty"` // НОВОЕ ПОЛЕ
	ExitPrice   float64 `json:"exit_price,omitempty"`
	ExitSize    float64 `json:"exit_size,omitempty"`

	RealizedPnL      float64 `json:"realized_pnl,omitempty"`
	RealizedPnLPct   float64 `json:"realized_pnl_pct,omitempty"`
	GrossRealizedPnL float64 `json:"gross_realized_pnl,omitempty"`
	TotalFees        float64 `json:"total_fees,omitempty"`
	PlannedRiskUSDT  float64 `json:"planned_risk_usdt,omitempty"`
	EffectiveR       float64 `json:"effective_r,omitempty"`
	ExitPriceR       float64 `json:"exit_price_r,omitempty"`
	EntrySlippageBps float64 `json:"entry_slippage_bps,omitempty"`

	RiskDist    float64 `json:"risk_dist,omitempty"`
	RMultiple   float64 `json:"r_multiple,omitempty"`
	DurationSec int64   `json:"duration_sec,omitempty"`

	MovedToBE          bool `json:"moved_to_be,omitempty"`
	LockedProfit       bool `json:"locked_profit,omitempty"`
	TookPartial        bool `json:"took_partial,omitempty"`
	PartialCount       int  `json:"partial_count,omitempty"`
	TimeStopTriggered  bool `json:"time_stop_triggered,omitempty"`
	SLReplaceAttempts  int  `json:"sl_replace_attempts,omitempty"`
	SLReplaceFailures  int  `json:"sl_replace_failures,omitempty"`
	TPReplaceAttempts  int  `json:"tp_replace_attempts,omitempty"`
	TPReplaceFailures  int  `json:"tp_replace_failures,omitempty"`
	AlgoCancelFailures int  `json:"algo_cancel_failures,omitempty"`
	BEReplaceAttempts  int  `json:"be_replace_attempts,omitempty"`
	BEReplaceFailures  int  `json:"be_replace_failures,omitempty"`

	MFEPrice float64 `json:"mfe_price,omitempty"`
	MAEPrice float64 `json:"mae_price,omitempty"`
	MFER     float64 `json:"mfe_r,omitempty"`
	MAER     float64 `json:"mae_r,omitempty"`
	CtVal    float64 `json:"ct_val"`
	// open trade runtime snapshot
	CurrentPrice       float64   `json:"current_price,omitempty"`
	CurrentSize        float64   `json:"current_size,omitempty"`
	ClosedSize         float64   `json:"closed_size,omitempty"`
	UnrealizedPnL      float64   `json:"unrealized_pnl,omitempty"`
	UnrealizedPnLPct   float64   `json:"unrealized_pnl_pct,omitempty"`
	PriceMovePct       float64   `json:"price_move_pct,omitempty"`
	ExchangeUPLRatio   float64   `json:"exchange_upl_ratio,omitempty"`
	PendingCloseReason string    `json:"pending_close_reason,omitempty"`
	BEPrice            float64   `json:"be_price,omitempty"`
	IsStale            bool      `json:"is_stale,omitempty"`
	StaleSince         time.Time `json:"stale_since,omitempty"`
	StaleMarkedAtR     float64   `json:"stale_marked_at_r,omitempty"`

	EntrySignalScore       int                 `json:"entry_signal_score,omitempty"`
	EntryOppositeScore     int                 `json:"entry_opposite_score,omitempty"`
	EntryRetestLevel       float64             `json:"entry_retest_level,omitempty"`
	EntryRetestDistancePct float64             `json:"entry_retest_distance_pct,omitempty"`
	EntryClosePos          float64             `json:"entry_close_pos,omitempty"`
	EntryImpulseBodyPct    float64             `json:"entry_impulse_body_pct,omitempty"`
	EntryHTFBias           string              `json:"entry_htf_bias,omitempty"`
	EntryChannelWidthPct   float64             `json:"entry_channel_width_pct,omitempty"`
	EntryCompressed        bool                `json:"entry_compressed,omitempty"`
	EntryVolatilityOK      bool                `json:"entry_volatility_ok,omitempty"`
	EntryVolumeRatio       float64             `json:"entry_volume_ratio,omitempty"`
	EntryRetestScore       int                 `json:"entry_retest_score,omitempty"`
	EntryCloseScore        int                 `json:"entry_close_score,omitempty"`
	EntryReclaimScore      int                 `json:"entry_reclaim_score,omitempty"`
	EntryImpulseScore      int                 `json:"entry_impulse_score,omitempty"`
	EntryStructureScore    int                 `json:"entry_structure_score,omitempty"`
	ConfigSnapshot         TradeConfigSnapshot `json:"config_snapshot,omitempty"`
}

type TradeConfigSnapshot struct {
	RiskPct              float64 `json:"risk_pct,omitempty"`
	Leverage             int     `json:"leverage,omitempty"`
	MinConfirmScore      int     `json:"min_confirm_score,omitempty"`
	RetestTolerancePct   float64 `json:"retest_tolerance_pct,omitempty"`
	ImpulseBodyMinPct    float64 `json:"impulse_body_min_pct,omitempty"`
	VolumeMinRatio       float64 `json:"volume_min_ratio,omitempty"`
	BETriggerR           float64 `json:"be_trigger_r,omitempty"`
	BEOffsetR            float64 `json:"be_offset_r,omitempty"`
	LockTriggerR         float64 `json:"lock_trigger_r,omitempty"`
	LockOffsetR          float64 `json:"lock_offset_r,omitempty"`
	PartialEnabled       bool    `json:"partial_enabled,omitempty"`
	PartialTriggerR      float64 `json:"partial_trigger_r,omitempty"`
	PartialCloseFrac     float64 `json:"partial_close_frac,omitempty"`
	EarlyTimeStopBars    int     `json:"early_time_stop_bars,omitempty"`
	EarlyTimeStopMinMFER float64 `json:"early_time_stop_min_mfe_r,omitempty"`
	TimeStopBars         int     `json:"time_stop_bars,omitempty"`
	TimeStopMinCurrentR  float64 `json:"time_stop_min_current_r,omitempty"`
	StaleAfterBars       int     `json:"stale_after_bars,omitempty"`
}

type TradeFillRole string

const (
	TradeFillRoleEntry TradeFillRole = "entry"
	TradeFillRoleExit  TradeFillRole = "exit"
)

type TradeFillRecord struct {
	TradeGUID   uuid.UUID     `json:"trade_guid"`
	TradeID     string        `json:"trade_id"`
	OrderID     string        `json:"order_id"`
	AlgoID      string        `json:"algo_id,omitempty"`
	InstID      string        `json:"inst_id"`
	PosSide     string        `json:"pos_side"`
	Side        string        `json:"side"`
	Role        TradeFillRole `json:"role"`
	FillPrice   float64       `json:"fill_price"`
	FillSize    float64       `json:"fill_size"`
	Fee         float64       `json:"fee"`
	RealizedPnL float64       `json:"realized_pnl"`
	FilledAt    time.Time     `json:"filled_at"`
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

type TradeCloseInput struct {
	ExitAt      time.Time
	CloseReason CloseReason
	Payload     TradePayload
}
