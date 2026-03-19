package models

import "time"

type PositionPhase string

const (
	PhaseNone          PositionPhase = "none"
	PhaseFresh         PositionPhase = "fresh"
	PhaseWorking       PositionPhase = "working"
	PhaseStalled       PositionPhase = "stalled"
	PhaseRecoveryWatch PositionPhase = "recovery_watch"
	PhaseInvalidated   PositionPhase = "invalidated"
)

type MarketBias string

const (
	MarketBiasBull    MarketBias = "bull"
	MarketBiasBear    MarketBias = "bear"
	MarketBiasNeutral MarketBias = "neutral"
)

type MarketContext struct {
	Bias             MarketBias `json:"bias"`
	Compressed       bool       `json:"compressed"`
	OverextendedUp   bool       `json:"overextended_up"`
	OverextendedDown bool       `json:"overextended_down"`
	VolatilityOK     bool       `json:"volatility_ok"`

	TrendStrength    float64 `json:"trend_strength"`
	ChannelWidthPct  float64 `json:"channel_width_pct"`
	DistanceToMidPct float64 `json:"distance_to_mid_pct"`
}

type SignalScore struct {
	SetupOK      bool `json:"setup_ok"`
	ContextOK    bool `json:"context_ok"`
	RetestOK     bool `json:"retest_ok"`
	StrongClose  bool `json:"strong_close"`
	ReclaimOK    bool `json:"reclaim_ok"`
	ImpulseOK    bool `json:"impulse_ok"`
	StructureOK  bool `json:"structure_ok"`
	VolatilityOK bool `json:"volatility_ok"`

	Score   int            `json:"score"`
	Reasons []RejectReason `json:"reasons,omitempty"`
}

type PendingSignal struct {
	Side      string    `json:"side"`
	Level     float64   `json:"level"`
	CreatedAt time.Time `json:"created_at"`
	ExpireAt  time.Time `json:"expire_at"`
	Score     int       `json:"score"`
}

type StrategyState struct {
	// уже есть
	PositionPhase    PositionPhase
	EntrySignalScore int
	LastSignalScore  int
	LastRetestLevel  float64
	LastRejectReason RejectReason `json:"last_reject_reason"`

	// NEW v3.4
	EntryPrice float64
	StopLoss   float64
	TakeProfit float64

	InitialRisk float64 // 1R

	PartialDone bool
	BEActivated bool

	TrailingStop float64

	LastUpdate time.Time
}
