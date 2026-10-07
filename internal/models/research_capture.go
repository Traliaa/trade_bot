package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
)

const ResearchSnapshotMaxBytes = 32 * 1024

type ResearchBuildIdentity struct {
	Revision string `json:"revision"`
	Dirty    bool   `json:"dirty"`
	Unknown  bool   `json:"unknown"`
}

// This explicit projection must never embed Settings: it contains trading keys.
type ResearchRules struct {
	BETriggerR           float64 `json:"be_trigger_r"`
	BEOffsetR            float64 `json:"be_offset_r"`
	LockTriggerR         float64 `json:"lock_trigger_r"`
	LockOffsetR          float64 `json:"lock_offset_r"`
	TimeStopBars         int     `json:"time_stop_bars"`
	TimeStopMinCurrentR  float64 `json:"time_stop_min_current_r"`
	EarlyTimeStopBars    int     `json:"early_time_stop_bars"`
	EarlyTimeStopMinMFER float64 `json:"early_time_stop_min_mfe_r"`
	PartialEnabled       bool    `json:"partial_enabled"`
	PartialTriggerR      float64 `json:"partial_trigger_r"`
	PartialCloseFrac     float64 `json:"partial_close_frac"`
	StaleAfterBars       int     `json:"stale_after_bars"`
	StaleMinMFER         float64 `json:"stale_min_mfe_r"`
	StaleExitProfitR     float64 `json:"stale_exit_profit_r"`
	StaleNearBER         float64 `json:"stale_near_be_r"`
	StaleMaxAdverseR     float64 `json:"stale_max_adverse_r"`
	StaleGraceBars       int     `json:"stale_grace_bars"`
	StaleWorseByR        float64 `json:"stale_worse_by_r"`
	StaleTightenToBER    float64 `json:"stale_tighten_to_be_r"`
}

type ResearchSettingsObservation struct {
	Stage            string        `json:"stage"`
	ObservedAt       time.Time     `json:"observed_at"`
	RelevantHash     string        `json:"relevant_hash"`
	Raw              ResearchRules `json:"raw"`
	Effective        ResearchRules `json:"effective"`
	RiskPct          float64       `json:"risk_pct"`
	EffectiveRiskPct float64       `json:"effective_risk_pct"`
	Leverage         int           `json:"leverage"`
}

type ResearchMetadataObservation struct {
	RawTickSz      string       `json:"raw_tick_sz"`
	RawLotSz       string       `json:"raw_lot_sz"`
	RawMinSz       string       `json:"raw_min_sz"`
	RawCtVal       string       `json:"raw_ct_val"`
	RawCtMult      string       `json:"raw_ct_mult"`
	TickSz         float64      `json:"tick_sz"`
	LotSz          float64      `json:"lot_sz"`
	MinSz          float64      `json:"min_sz"`
	EffectiveCtVal float64      `json:"effective_ct_val"`
	Kind           ContractKind `json:"kind"`
	SettleCcy      string       `json:"settle_ccy"`
	CtValCcy       string       `json:"ct_val_ccy"`
	ReceivedAt     time.Time    `json:"received_at"`
	ExchangeAt     *time.Time   `json:"exchange_at"`
}

type ResearchTradeValues struct {
	Entry    float64 `json:"entry"`
	Size     float64 `json:"size"`
	SL       float64 `json:"sl"`
	TP       float64 `json:"tp"`
	RiskDist float64 `json:"risk_dist"`
}
type ResearchEntryEvidence struct {
	Status      string     `json:"status"`
	TimeSource  string     `json:"time_source"`
	FirstFillAt *time.Time `json:"first_fill_at"`
	LastFillAt  *time.Time `json:"last_fill_at"`
	FilledSize  float64    `json:"filled_size"`
	FillCount   int        `json:"fill_count"`
	Reasons     []string   `json:"reasons"`
}
type ResearchEntryObservation struct {
	Planned  ResearchTradeValues           `json:"planned"`
	Metadata ResearchMetadataObservation   `json:"metadata"`
	Settings []ResearchSettingsObservation `json:"settings"`
}
type ResearchCaptureInput struct {
	CaptureID   string                   `json:"capture_id"`
	ProtocolID  string                   `json:"protocol_id"`
	Symbol      string                   `json:"symbol"`
	Side        string                   `json:"side"`
	Timeframe   string                   `json:"timeframe"`
	Build       ResearchBuildIdentity    `json:"build"`
	EntryAt     time.Time                `json:"entry_at"`
	Observation ResearchEntryObservation `json:"observation"`
	Actual      ResearchTradeValues      `json:"actual"`
	Evidence    ResearchEntryEvidence    `json:"evidence"`
}

func ProjectResearchSettings(stage string, at time.Time, cfg Settings, effectiveRiskPct float64) ResearchSettingsObservation {
	tc := cfg.TrailingConfig
	raw := ResearchRules{
		BETriggerR: tc.BETriggerR, BEOffsetR: tc.BEOffsetR, LockTriggerR: tc.LockTriggerR, LockOffsetR: tc.LockOffsetR,
		TimeStopBars: tc.TimeStopBars, TimeStopMinCurrentR: tc.TimeStopMinCurrentR, EarlyTimeStopBars: tc.EarlyTimeStopBars, EarlyTimeStopMinMFER: tc.EarlyTimeStopMinMFER,
		PartialEnabled: tc.PartialEnabled, PartialTriggerR: tc.PartialTriggerR, PartialCloseFrac: tc.PartialCloseFrac,
		StaleAfterBars: tc.StaleAfterBars, StaleMinMFER: tc.StaleMinMFER, StaleExitProfitR: tc.StaleExitProfitR, StaleNearBER: tc.StaleNearBER, StaleMaxAdverseR: tc.StaleMaxAdverseR, StaleGraceBars: tc.StaleGraceBars, StaleWorseByR: tc.StaleWorseByR, StaleTightenToBER: tc.StaleTightenToBER,
	}
	effective := raw
	sc := GetStaleConfig(cfg)
	effective.StaleAfterBars = sc.AfterBars
	effective.StaleMinMFER = sc.MinMFER
	effective.StaleExitProfitR = sc.ExitProfitR
	effective.StaleNearBER = sc.NearBER
	effective.StaleMaxAdverseR = sc.MaxAdverseR
	effective.StaleGraceBars = sc.GraceBars
	effective.StaleWorseByR = sc.WorseByR
	effective.StaleTightenToBER = sc.TightenToBER
	o := ResearchSettingsObservation{Stage: stage, ObservedAt: at.UTC(), Raw: raw, Effective: effective, RiskPct: cfg.TradingSettings.RiskPct, EffectiveRiskPct: effectiveRiskPct, Leverage: cfg.TradingSettings.Leverage}
	o.RelevantHash = researchSettingsHash(o)
	return o
}

func researchSettingsHash(o ResearchSettingsObservation) string {
	o.Stage = ""
	o.ObservedAt = time.Time{}
	o.RelevantHash = ""
	raw, err := json.Marshal(o)
	if err != nil {
		return ""
	}
	return researchChecksum(raw)
}
func researchChecksum(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var researchProtocol = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var researchSymbol = regexp.MustCompile(`^[A-Z0-9-]{1,64}$`)
var researchDecimal = regexp.MustCompile(`^[0-9]{1,32}(\.[0-9]{1,32})?$`)
var researchRevision = regexp.MustCompile(`^[a-fA-F0-9]{7,64}$`)
var researchTF = regexp.MustCompile(`^[1-9][0-9]{0,3}[mhdwMHDW]$`)

// BuildResearchEntrySnapshot returns only bounded error codes, never input/error
// text. The envelope checksum covers compact payload bytes, not itself. JSONB
// consumers must restore this typed field order before recomputing the checksum.
func BuildResearchEntrySnapshot(in ResearchCaptureInput) (json.RawMessage, string) {
	id, err := uuid.Parse(in.CaptureID)
	if err != nil || id.Version() != 4 || id.Variant() != uuid.RFC4122 {
		return nil, "invalid_capture_id"
	}
	if !researchProtocol.MatchString(in.ProtocolID) {
		return nil, "invalid_protocol"
	}
	if !researchSymbol.MatchString(in.Symbol) || !researchTF.MatchString(in.Timeframe) || (in.Side != "long" && in.Side != "short") {
		return nil, "invalid_identity"
	}
	if in.Build.Revision != "" && !researchRevision.MatchString(in.Build.Revision) {
		return nil, "invalid_build"
	}
	reasons := []string{}
	add := func(s string) { reasons = append(reasons, s) }
	if in.Build.Unknown || in.Build.Revision == "" {
		in.Build.Unknown = true
		add("build_unknown")
	}
	in.EntryAt = in.EntryAt.UTC()
	if in.EntryAt.IsZero() {
		add("entry_time_missing")
	}
	in.Observation.Settings = append([]ResearchSettingsObservation(nil), in.Observation.Settings...)
	seen := map[string]bool{}
	hash := ""
	for i := range in.Observation.Settings {
		o := &in.Observation.Settings[i]
		if o.Stage != "calc" && o.Stage != "sizing" && o.Stage != "open" {
			return nil, "invalid_stage"
		}
		if seen[o.Stage] {
			add("settings_incomplete")
		}
		seen[o.Stage] = true
		o.ObservedAt = o.ObservedAt.UTC()
		if o.ObservedAt.IsZero() {
			add("settings_incomplete")
		}
		o.RelevantHash = researchSettingsHash(*o)
		if hash != "" && hash != o.RelevantHash {
			add("settings_changed_during_entry")
		}
		hash = o.RelevantHash
	}
	if len(seen) != 3 {
		add("settings_incomplete")
	}
	m := &in.Observation.Metadata
	m.ReceivedAt = m.ReceivedAt.UTC()
	m.ExchangeAt = researchUTC(m.ExchangeAt)
	for _, s := range []string{m.RawTickSz, m.RawLotSz, m.RawMinSz, m.RawCtVal, m.RawCtMult} {
		if s == "" {
			add("metadata_incomplete")
		} else if !researchDecimal.MatchString(s) {
			return nil, "invalid_metadata"
		}
	}
	for _, s := range []string{m.SettleCcy, m.CtValCcy} {
		if s == "" {
			add("metadata_incomplete")
		} else if !researchSymbol.MatchString(s) {
			return nil, "invalid_metadata"
		}
	}
	if m.ReceivedAt.IsZero() || m.TickSz <= 0 || m.LotSz <= 0 || m.MinSz <= 0 || m.EffectiveCtVal <= 0 || m.Kind == ContractUnknown {
		add("metadata_incomplete")
	}
	e := &in.Evidence
	if e.Status != "reported_complete" && e.Status != "incomplete" {
		return nil, "invalid_evidence"
	}
	if e.TimeSource != "exchange_fills" && e.TimeSource != "exchange_record" && e.TimeSource != "local_fallback" {
		return nil, "invalid_evidence"
	}
	for _, r := range e.Reasons {
		switch r {
		case "fills_unavailable", "fill_volume_unverified", "fill_time_missing", "invalid_fill", "multi_time_entry", "entry_time_not_execution":
			add(r)
		default:
			return nil, "invalid_evidence"
		}
	}
	e.FirstFillAt = researchUTC(e.FirstFillAt)
	e.LastFillAt = researchUTC(e.LastFillAt)
	if e.Status != "reported_complete" || e.TimeSource == "local_fallback" || e.FillCount <= 0 || e.FirstFillAt == nil || e.LastFillAt == nil {
		add("fills_incomplete")
	}
	if e.FirstFillAt != nil && e.LastFillAt != nil && !e.FirstFillAt.Equal(*e.LastFillAt) {
		add("multi_time_entry")
	}
	e.Reasons = researchReasons(e.Reasons)
	reasons = researchReasons(reasons)
	status := "complete"
	if len(reasons) > 0 {
		status = "incomplete"
	}
	payload, err := json.Marshal(struct {
		Schema        int      `json:"schema"`
		CaptureStatus string   `json:"capture_status"`
		Reasons       []string `json:"reasons"`
		ResearchCaptureInput
	}{1, status, reasons, in})
	if err != nil {
		return nil, "encode_failed"
	}
	out, err := json.Marshal(struct {
		Payload  json.RawMessage `json:"payload"`
		Checksum string          `json:"checksum"`
	}{payload, researchChecksum(payload)})
	if err != nil {
		return nil, "encode_failed"
	}
	if len(out) > ResearchSnapshotMaxBytes {
		return nil, "snapshot_too_large"
	}
	return out, ""
}

func researchUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.UTC()
	return &v
}
func researchReasons(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	result := []string{}
	for _, s := range out {
		if len(result) == 0 || result[len(result)-1] != s {
			result = append(result, s)
		}
	}
	return result
}
