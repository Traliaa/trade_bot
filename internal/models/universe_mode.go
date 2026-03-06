package models

type UniverseMode string

const (
	UniverseConservative UniverseMode = "conservative"
	UniverseAggressive   UniverseMode = "aggressive"
)

type UniverseLimits struct {
	MinLast     float64
	MinVolCcy24 float64
	MaxRangePct float64
	MaxMovePct  float64
}

func LimitsForMode(mode UniverseMode) UniverseLimits {
	switch mode {
	case UniverseAggressive:
		return UniverseLimits{
			MinLast:     0.02,
			MinVolCcy24: 5_000_000,
			MaxRangePct: 0.25,
			MaxMovePct:  0.15,
		}

	case UniverseConservative:
		fallthrough
	default:
		return UniverseLimits{
			MinLast:     0.05,
			MinVolCcy24: 10_000_000,
			MaxRangePct: 0.20,
			MaxMovePct:  0.12,
		}
	}
}
