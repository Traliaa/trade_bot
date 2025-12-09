package strategy

// Side как у тебя в раннере: "BUY"/"SELL" или пустая строка.
type Side string

const (
	SideNone Side = ""
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// Candle — универсальная свеча для стратегий.
type Candle struct {
	Open, High, Low, Close float64
}

// Signal — ответ стратегии.
type Signal struct {
	Symbol string
	Side   Side // BUY / SELL / ""
	Price  float64
	Reason string
}

// Engine — то, что Runner будет дергать.
type Engine interface {
	OnCandle(symbol string, c Candle) Signal
	Dump(symbol string) string
}
