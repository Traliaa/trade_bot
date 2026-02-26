package models

type Trend int

const (
	TrendNone Trend = iota
	TrendUp
	TrendDown
)

func (t Trend) String() string {
	switch t {
	case TrendUp:
		return "up"
	case TrendDown:
		return "down"
	default:
		return "none"
	}
}
