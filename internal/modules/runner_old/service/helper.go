package service

import "trade_bot/internal/models"

func countOpenSides(pos map[models.PosKey]*models.PositionTrailState) (longs, shorts int) {
	for _, p := range pos {
		if p == nil {
			continue
		}

		switch p.PosSide {
		case "long":
			longs++
		case "short":
			shorts++
		}
	}

	return longs, shorts
}
