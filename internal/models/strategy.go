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

type PendingEntry struct {
	Active   bool
	Side     Side
	Level    float64 // уровень пробоя (dh для buy, dl для sell)
	Created  time.Time
	ExpireAt time.Time

	// доп. инфа для дебага/фильтров
	BreakCandleEnd time.Time
	BreakClosePos  float64 // closePos пробойной свечи (не обязательно)
}
