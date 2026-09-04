package models

import "time"

type StrategyType string

const (
	StrategyEMARSI     StrategyType = "emarsi"
	StrategyDonchian   StrategyType = "donchian"
	StrategyDonchianV2 StrategyType = "donchianV2"
	StrategyDonchianV3 StrategyType = "donchian_v3_smart"
)

type Signal struct {
	InstID      string
	TF          string // "15m"
	Side        Side   // "BUY" / "SELL"
	Price       float64
	Strategy    StrategyType // "donchian_v2_htf1h"
	Reason      string
	CreatedAt   time.Time
	LTFCandles  []CandleTick
	HTFCandles  []CandleTick
	Diagnostics SignalDiagnostics
}

type SignalDiagnostics struct {
	Score             int
	OppositeScore     int
	RetestLevel       float64
	RetestDistancePct float64
	ClosePos          float64
	ImpulseBodyPct    float64
	HTFBias           MarketBias
	ChannelWidthPct   float64
	Compressed        bool
	VolatilityOK      bool
	VolumeRatio       float64
	RetestScore       int
	CloseScore        int
	ReclaimScore      int
	ImpulseScore      int
	StructureScore    int
}

// Side как у тебя в раннере: "BUY"/"SELL" или пустая строка.
type Side string

const (
	SideNone Side = ""
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)
