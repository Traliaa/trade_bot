package models

import "time"

type TradeReconciliationReport struct {
	Scope               string                `json:"scope"`
	FundingStatus       string                `json:"funding_status"`
	CheckedAt           time.Time             `json:"checked_at"`
	Verified            int64                 `json:"verified"`
	Incomplete          int64                 `json:"incomplete"`
	Mismatched          int64                 `json:"mismatched"`
	VerifiedStats       *ReconciledMoneyStats `json:"verified_stats"`
	VerifiedStatsStatus string                `json:"verified_stats_status"`
	Trades              []TradeReconciliation `json:"trades"`
}

// Only monetary fields are reconciled; no claim is made about legacy R metrics.
type ReconciledMoneyStats struct {
	Trades       int64    `json:"trades"`
	Wins         int64    `json:"wins"`
	Losses       int64    `json:"losses"`
	Breakeven    int64    `json:"breakeven"`
	WinRate      float64  `json:"win_rate"`
	NetPnL       float64  `json:"net_pnl"`
	Fees         float64  `json:"fees"`
	AvgPnL       float64  `json:"avg_pnl"`
	ProfitFactor *float64 `json:"profit_factor"`
}
