package models

import "time"

type StrategyType string

const (
	StrategyEMARSI     StrategyType = "emarsi"
	StrategyDonchian   StrategyType = "donchian"
	StrategyDonchianV2 StrategyType = "donchianV2"
)

type Signal struct {
	InstID    string
	TF        string // "15m"
	Side      Side   // "BUY" / "SELL"
	Price     float64
	Strategy  StrategyType // "donchian_v2_htf1h"
	Reason    string
	CreatedAt time.Time
}

// Side как у тебя в раннере: "BUY"/"SELL" или пустая строка.
type Side string

const (
	SideNone Side = ""
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)
