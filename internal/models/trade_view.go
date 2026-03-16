package models

import "time"

type TradeListItem struct {
	GUID      string      `json:"guid"`
	InstID    string      `json:"inst_id"`
	PosSide   string      `json:"pos_side"`
	Side      string      `json:"side"`
	Timeframe string      `json:"timeframe"`
	Strategy  string      `json:"strategy"`
	Status    TradeStatus `json:"status"`

	EntryPrice float64    `json:"entry_price"`
	ExitPrice  float64    `json:"exit_price"`
	EntryAt    time.Time  `json:"entry_at"`
	ExitAt     *time.Time `json:"exit_at,omitempty"`

	RealizedPnL    float64     `json:"realized_pnl"`
	RealizedPnLPct float64     `json:"realized_pnl_pct"`
	RMultiple      float64     `json:"r_multiple"`
	DurationSec    int64       `json:"duration_sec"`
	CloseReason    CloseReason `json:"close_reason"`

	MovedToBE    bool    `json:"moved_to_be"`
	LockedProfit bool    `json:"locked_profit"`
	TookPartial  bool    `json:"took_partial"`
	MFER         float64 `json:"mfe_r"`
	MAER         float64 `json:"mae_r"`
}

func NewTradeListItem(tr TradeRecord) TradeListItem {
	p := tr.Payload

	return TradeListItem{
		GUID:      tr.GUID.String(),
		InstID:    tr.InstID,
		PosSide:   p.PosSide,
		Side:      p.Side,
		Timeframe: tr.Timeframe,
		Strategy:  tr.Strategy,
		Status:    tr.Status,

		EntryPrice: p.EntryPrice,
		ExitPrice:  p.ExitPrice,

		EntryAt: tr.EntryAt,
		ExitAt:  tr.ExitAt,

		RealizedPnL:    p.RealizedPnL,
		RealizedPnLPct: p.RealizedPnLPct,

		CloseReason: tr.CloseReason,

		RMultiple:   p.RMultiple,
		DurationSec: p.DurationSec,
	}
}
func NewTradeListItems(records []TradeRecord) []TradeListItem {
	out := make([]TradeListItem, 0, len(records))

	for _, tr := range records {
		out = append(out, NewTradeListItem(tr))
	}

	return out
}
