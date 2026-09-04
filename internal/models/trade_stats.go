package models

type TradeStats struct {
	TotalTrades  int64 `json:"total_trades"`
	OpenTrades   int64 `json:"open_trades"`
	ClosedTrades int64 `json:"closed_trades"`

	Wins            int64   `json:"wins"`
	Losses          int64   `json:"losses"`
	BreakevenTrades int64   `json:"breakeven_trades"`
	WinRate         float64 `json:"win_rate"`

	TotalPnL     float64 `json:"total_pnl"`
	AvgPnL       float64 `json:"avg_pnl"`
	ProfitFactor float64 `json:"profit_factor"`

	TotalR  float64 `json:"total_r"`
	AvgR    float64 `json:"avg_r"`
	MedianR float64 `json:"median_r"`

	AvgDurationSec int64 `json:"avg_duration_sec"`

	AvgMFER float64 `json:"avg_mfe_r"`
	AvgMAER float64 `json:"avg_mae_r"`

	TPCount            int64 `json:"tp_count"`
	SLCount            int64 `json:"sl_count"`
	BreakEvenCount     int64 `json:"break_even_count"`
	LockProfitCount    int64 `json:"lock_profit_count"`
	PartialExitCount   int64 `json:"partial_exit_count"`
	TimeStopCount      int64 `json:"time_stop_count"`
	TimeStopEarlyCount int64 `json:"time_stop_early_count"`
	TimeStopStaleCount int64 `json:"time_stop_stale_count"`
	ManualCloseCount   int64 `json:"manual_close_count"`
	RecoveryCloseCount int64 `json:"recovery_close_count"`
	ForceCloseCount    int64 `json:"force_close_count"`
	UnknownCloseCount  int64 `json:"unknown_close_count"`

	PartialTrades      int64 `json:"partial_trades"`
	SLReplaceAttempts  int64 `json:"sl_replace_attempts"`
	SLReplaceFailures  int64 `json:"sl_replace_failures"`
	TPReplaceAttempts  int64 `json:"tp_replace_attempts"`
	TPReplaceFailures  int64 `json:"tp_replace_failures"`
	AlgoCancelFailures int64 `json:"algo_cancel_failures"`
	BEReplaceAttempts  int64 `json:"be_replace_attempts"`
	BEReplaceFailures  int64 `json:"be_replace_failures"`

	BestTradeR  float64 `json:"best_trade_r"`
	WorstTradeR float64 `json:"worst_trade_r"`

	OpenPnL float64 `json:"open_pnl,omitempty"`

	ByDirection   []TradeStatsBreakdown    `json:"by_direction,omitempty"`
	BySignalScore []TradeStatsBreakdown    `json:"by_signal_score,omitempty"`
	ByImpulse     []TradeStatsBreakdown    `json:"by_impulse,omitempty"`
	ByCloseReason []TradeStatsBreakdown    `json:"by_close_reason,omitempty"`
	ByMonth       []TradeStatsBreakdown    `json:"by_month,omitempty"`
	Windows       []TradePerformanceWindow `json:"windows,omitempty"`
}

type TradeStatsBreakdown struct {
	Key          string  `json:"key"`
	Trades       int64   `json:"trades"`
	Wins         int64   `json:"wins"`
	Losses       int64   `json:"losses"`
	WinRate      float64 `json:"win_rate"`
	TotalPnL     float64 `json:"total_pnl"`
	ProfitFactor float64 `json:"profit_factor"`
	TotalR       float64 `json:"total_r"`
	AvgR         float64 `json:"avg_r"`
}

type TradePerformanceWindow struct {
	Key          string  `json:"key"`
	Trades       int64   `json:"trades"`
	Wins         int64   `json:"wins"`
	Losses       int64   `json:"losses"`
	WinRate      float64 `json:"win_rate"`
	TotalPnL     float64 `json:"total_pnl"`
	ProfitFactor float64 `json:"profit_factor"`
	TotalR       float64 `json:"total_r"`
	AvgR         float64 `json:"avg_r"`
	From         string  `json:"from,omitempty"`
	To           string  `json:"to,omitempty"`
}
