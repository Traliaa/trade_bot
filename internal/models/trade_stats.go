package models

type TradeStats struct {
	TotalTrades  int
	OpenTrades   int
	ClosedTrades int

	Wins   int
	Losses int

	WinRatePct float64

	TotalPnL float64
	AvgPnL   float64
	AvgWin   float64
	AvgLoss  float64

	TPCount       int
	SLCount       int
	TimeStopCount int
	PartialCount  int
	ManualCount   int
	UnknownCount  int
}
