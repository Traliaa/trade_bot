package service

import (
	"context"
	"encoding/json"
	"runtime/debug"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"

	"github.com/google/uuid"
)

// Initialized before trading starts; no build inspection on the protection path.
var cachedResearchBuild = researchBuildIdentity()

func researchBuildIdentity() models.ResearchBuildIdentity {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return researchBuildIdentityFromInfo(nil)
	}
	return researchBuildIdentityFromInfo(info)
}
func researchBuildIdentityFromInfo(info *debug.BuildInfo) models.ResearchBuildIdentity {
	out := models.ResearchBuildIdentity{Unknown: true}
	if info == nil {
		return out
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			out.Revision = s.Value
			out.Unknown = s.Value == ""
		case "vcs.modified":
			out.Dirty = s.Value == "true"
		}
	}
	return out
}

func prepareResearchEntrySnapshot(cfg config.ResearchCaptureConfig, tr models.TradeRecord, params *models.TradeParams, opened *models.OpenResult, build models.ResearchBuildIdentity, newCaptureID func() (string, error)) (json.RawMessage, string) {
	if !cfg.Enabled {
		return nil, ""
	}
	if params == nil || opened == nil || params.ResearchEntry == nil || opened.ResearchEvidence == nil {
		return nil, "missing_observation"
	}
	id, err := newCaptureID()
	if err != nil {
		return nil, "capture_id_failed"
	}
	return models.BuildResearchEntrySnapshot(models.ResearchCaptureInput{
		CaptureID: id, ProtocolID: cfg.ProtocolID, Symbol: tr.InstID, Side: opened.PosSide, Timeframe: tr.Timeframe, Build: build, EntryAt: opened.EntryAt,
		Observation: *params.ResearchEntry,
		Actual:      models.ResearchTradeValues{Entry: params.Entry, Size: params.Size, SL: params.SL, TP: params.TP, RiskDist: params.RiskDist}, Evidence: *opened.ResearchEvidence,
	})
}

// The only side effect is the existing create callback. No retry or transaction
// is introduced; capture failure must never turn a successful entry into an
// unrecorded trade. Keeping this boundary small also permits failure injection.
type researchEntryRecorder struct {
	cfg    config.ResearchCaptureConfig
	build  models.ResearchBuildIdentity
	newID  func() (string, error)
	count  func(string)
	create func(context.Context, models.TradeRecord) error
}

func (r researchEntryRecorder) save(ctx context.Context, tr models.TradeRecord, params *models.TradeParams, opened *models.OpenResult) error {
	raw, code := prepareResearchEntrySnapshot(r.cfg, tr, params, opened, r.build, r.newID)
	tr.ResearchEntrySnapshot = raw
	switch {
	case !r.cfg.Enabled:
		r.count("research_capture_disabled")
	case code != "":
		r.count("research_capture_failed")
		r.count("research_capture_error_" + code)
	default:
		var envelope struct {
			Payload struct {
				Status string `json:"capture_status"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Payload.Status == "complete" {
			r.count("research_capture_complete")
		} else {
			r.count("research_capture_incomplete")
		}
	}
	return r.create(ctx, tr)
}

func newResearchCaptureID() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}
