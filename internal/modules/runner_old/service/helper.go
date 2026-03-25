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
func hasOpenInstID(trails map[models.PosKey]*models.PositionTrailState, instID string) bool {
	for key, st := range trails {
		if key.InstID != instID {
			continue
		}
		if st == nil {
			continue
		}
		return true
	}
	return false
}

func hasOpenInstIDSide(trails map[models.PosKey]*models.PositionTrailState, instID, posSide string) bool {
	for key, st := range trails {
		if key.InstID != instID {
			continue
		}
		if key.PosSide != posSide {
			continue
		}
		if st == nil {
			continue
		}
		return true
	}
	return false
}
