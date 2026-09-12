package research

import (
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/universe"
)

// Dataset deliberately contains no account IDs, credentials or executable code.
// Snapshots must be historically recorded, not today's ticker list relabeled.
type Dataset struct {
	Schema          int                   `json:"schema"`
	Provenance      string                `json:"provenance"`
	Strategy        config.StrategyConfig `json:"strategy_config"`
	Bars            []models.CandleTick   `json:"bars"`
	Snapshots       []Snapshot            `json:"snapshots"`
	Funding         []Funding             `json:"funding"`
	FundingComplete bool                  `json:"funding_complete"`
}
type Snapshot struct {
	At         time.Time            `json:"at"`
	Candidates []universe.Candidate `json:"candidates"`
}
type Funding struct {
	At     time.Time `json:"at"`
	Symbol string    `json:"symbol"`
	Rate   float64   `json:"rate"`
	Mark   float64   `json:"mark"`
}
type Options struct {
	Strategy       string          `json:"strategy"`
	Policy         universe.Policy `json:"policy"`
	Start          time.Time       `json:"start"`
	End            time.Time       `json:"end"`
	Equity         float64         `json:"equity"`
	RiskPct        float64         `json:"risk_pct"`
	Leverage       float64         `json:"leverage"`
	FeeBPS         float64         `json:"fee_bps"`
	SlippageBPS    float64         `json:"slippage_bps"`
	MaxPositions   int             `json:"max_positions"`
	MaxHolding     time.Duration   `json:"max_holding"`
	RR             float64         `json:"rr"`
	Shorts         bool            `json:"shorts"`
	SnapshotMaxAge time.Duration   `json:"snapshot_max_age"`
}
type Trade struct {
	SpreadBPS                                                                                    float64     `json:"spread_bps"`
	Symbol                                                                                       string      `json:"symbol"`
	Side                                                                                         models.Side `json:"side"`
	OpenAt                                                                                       time.Time   `json:"open_at"`
	CloseAt                                                                                      time.Time   `json:"close_at"`
	Entry, Exit, Contracts, ContractValue, Stop, Target, Margin, Fees, Funding, Gross, Net, Risk float64
	Reason                                                                                       string `json:"reason"`
}
type Report struct {
	MeanDailyNet95    *Interval             `json:"mean_daily_net_95"`
	EffectiveStrategy config.StrategyConfig `json:"effective_strategy_config"`
	Options           Options               `json:"options"`
	Trades            []Trade               `json:"trades"`
	Funnel            map[string]int        `json:"funnel"`
	Net               float64               `json:"net"`
	MaxDrawdown       float64               `json:"max_drawdown_usdt"`
	WinRate           float64               `json:"win_rate"`
	ProfitFactor      *float64              `json:"profit_factor"`
	Expectancy        float64               `json:"expectancy_usdt"`
	Turnover          float64               `json:"turnover_usdt"`
	Warnings          []string              `json:"warnings"`
}
