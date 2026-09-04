package sessions

import (
	"sort"
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

func pickCloseFills(fills []models.TradeFill, tr models.TradeRecord) []models.TradeFill {
	wantSide := ""
	switch tr.Payload.PosSide {
	case "long":
		wantSide = "sell"
	case "short":
		wantSide = "buy"
	default:
		return nil
	}

	result := make([]models.TradeFill, 0, len(fills))
	for _, fill := range fills {
		if fill.InstID != tr.InstID || fill.FillTime.Before(tr.EntryAt) {
			continue
		}
		if strings.ToLower(fill.Side) != wantSide {
			continue
		}
		if fill.PosSide != "" && fill.PosSide != tr.Payload.PosSide {
			continue
		}
		result = append(result, fill)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].FillTime.Before(result[j].FillTime)
	})
	return result
}
