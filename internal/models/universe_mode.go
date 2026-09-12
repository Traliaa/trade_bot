package models

type UniverseMode string

const (
	UniverseConservative UniverseMode = "conservative"
	UniverseAggressive   UniverseMode = "aggressive"
)

type UniverseLimits struct {
	MinQuoteVolumeUSDT float64
	MaxRangePct        float64
	MaxMovePct         float64
}

func LimitsForMode(mode UniverseMode) UniverseLimits {
	switch mode {
	case UniverseAggressive:
		return UniverseLimits{
			MinQuoteVolumeUSDT: 5_000_000,
			MaxRangePct:        0.25,
			MaxMovePct:         0.15,
		}

	case UniverseConservative:
		fallthrough
	default:
		return UniverseLimits{
			MinQuoteVolumeUSDT: 10_000_000,
			MaxRangePct:        0.20,
			MaxMovePct:         0.12,
		}
	}
}
