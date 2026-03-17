package sessions

import (
	"strings"

	"trade_bot/internal/models"
)

func pickCloseFill(fills []models.TradeFill, tr models.TradeRecord) *models.TradeFill {
	wantSide := ""
	switch tr.Payload.PosSide {
	case "long":
		wantSide = "sell"
	case "short":
		wantSide = "buy"
	default:
		return nil
	}

	var best *models.TradeFill
	for i := range fills {
		f := &fills[i]

		if f.InstID != tr.InstID {
			continue
		}
		if f.FillTime.Before(tr.EntryAt) {
			continue
		}
		if strings.ToLower(f.Side) != wantSide {
			continue
		}

		// если posSide в fills приходит — тоже сверяем
		if f.PosSide != "" && f.PosSide != tr.Payload.PosSide {
			continue
		}

		if best == nil || f.FillTime.After(best.FillTime) {
			best = f
		}
	}

	return best
}
