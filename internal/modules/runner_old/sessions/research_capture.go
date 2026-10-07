package sessions

import (
	"math"
	"time"
	"trade_bot/internal/models"
)

func (s *UserSession) observeResearchSettings(stage string, cfg models.Settings) *models.ResearchSettingsObservation {
	if s.Config == nil || !s.Config.ResearchCapture.Enabled {
		return nil
	}
	o := models.ProjectResearchSettings(stage, time.Now().UTC(), cfg, s.EffectiveRiskPct(cfg.TradingSettings.RiskPct))
	return &o
}

func (s *UserSession) researchEntryObservation(p *models.TradeParams, meta models.Instrument, calc *models.ResearchSettingsObservation) *models.ResearchEntryObservation {
	if s.Config == nil || !s.Config.ResearchCapture.Enabled {
		return nil
	}
	o := &models.ResearchEntryObservation{Planned: models.ResearchTradeValues{Entry: p.Entry, Size: p.Size, SL: p.SL, TP: p.TP, RiskDist: p.RiskDist}, Settings: []models.ResearchSettingsObservation{}}
	if meta.ResearchMetadata != nil {
		o.Metadata = *meta.ResearchMetadata
	}
	if calc != nil {
		o.Settings = append(o.Settings, *calc)
	}
	if p.SizeMeta != nil && p.SizeMeta.ResearchSettings != nil {
		o.Settings = append(o.Settings, *p.SizeMeta.ResearchSettings)
	}
	return o
}

// Source-reported float fills are evidence, not decimal archive reconciliation.
// In particular WaitOrderFills can return a shortfall with nil error on timeout.
func researchFillEvidence(fills []models.TradeFill, expected float64, fillErr error) *models.ResearchEntryEvidence {
	e := &models.ResearchEntryEvidence{Status: "reported_complete", TimeSource: "exchange_fills", FillCount: len(fills), Reasons: []string{}}
	incomplete := func(r string) { e.Status = "incomplete"; e.Reasons = append(e.Reasons, r) }
	if fillErr != nil || len(fills) == 0 {
		incomplete("fills_unavailable")
		e.TimeSource = "local_fallback"
	}
	validPrice := false
	for _, f := range fills {
		if f.FillPx <= 0 || f.FillSz <= 0 || math.IsNaN(f.FillPx) || math.IsNaN(f.FillSz) || math.IsInf(f.FillPx, 0) || math.IsInf(f.FillSz, 0) {
			incomplete("invalid_fill")
			continue
		}
		validPrice = true
		e.FilledSize += f.FillSz
		if f.FillTime.IsZero() {
			incomplete("fill_time_missing")
			continue
		}
		at := f.FillTime.UTC()
		if e.FirstFillAt == nil || at.Before(*e.FirstFillAt) {
			v := at
			e.FirstFillAt = &v
		}
		if e.LastFillAt == nil || at.After(*e.LastFillAt) {
			v := at
			e.LastFillAt = &v
		}
	}
	if !validPrice || e.LastFillAt == nil {
		e.TimeSource = "local_fallback"
	}
	if e.FilledSize != expected || expected <= 0 {
		incomplete("fill_volume_unverified")
	}
	if e.FirstFillAt != nil && e.LastFillAt != nil && !e.FirstFillAt.Equal(*e.LastFillAt) {
		incomplete("multi_time_entry")
	}
	return e
}
