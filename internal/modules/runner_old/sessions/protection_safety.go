package sessions

import (
	"fmt"
	"math"
	"time"
	"trade_bot/internal/models"
)

func rejectExistingExchangePosition(positions []models.OpenPosition, instID string) error {
	for _, p := range positions {
		if p.Symbol == instID && p.Size != 0 {
			return fmt.Errorf("exchange position already exists: inst=%s side=%s size=%g; new entry skipped", instID, p.Side, p.Size)
		}
	}
	return nil
}

// An unexecutable partial must not prevent independent SL improvements.
func decisionWithoutPartial(st *models.PositionTrailState, cfg models.Settings, price float64, now time.Time) models.TrailDecision {
	cfg.TrailingConfig.PartialEnabled = false
	return decideTrail15m(st, cfg, price, now)
}

func normalizePartialDecision(st *models.PositionTrailState, cfg models.Settings, price float64, now time.Time, dec models.TrailDecision, meta models.Instrument) models.TrailDecision {
	if dec.CloseSize <= 0 {
		return dec
	}
	if size := normalizedPartialSize(st.Size, dec.CloseSize, meta); size > 0 {
		dec.CloseSize = size
		return dec
	}
	return decisionWithoutPartial(st, cfg, price, now)
}

func normalizedPartialSize(positionSize, requested float64, meta models.Instrument) float64 {
	finitePositive := func(v float64) bool { return v > 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
	if finitePositive(meta.LotSz) && finitePositive(meta.MinSz) && finitePositive(requested) && finitePositive(positionSize) {
		size, err := normalizeSize(requested, meta.LotSz, meta.MinSz, meta.MaxMktSz)
		remaining := positionSize - size
		if err == nil && size > 0 && remaining+meta.LotSz*1e-9 >= meta.MinSz && remaining > 0 {
			return size
		}
	}
	return 0
}
