package models

type StrategyType string

const (
	StrategyEMARSI   StrategyType = "emarsi"
	StrategyDonchian StrategyType = "donchian"
)

type Signal struct {
	Symbol    string
	Timeframe string
	Side      Side // "BUY"/"SELL"
	Price     float64
	Strategy  StrategyType
	Reason    string
}

// Side как у тебя в раннере: "BUY"/"SELL" или пустая строка.
type Side string

const (
	SideNone Side = ""
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)
