package models

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func captureFixture() ResearchCaptureInput {
	at := time.Date(2026, 10, 7, 8, 1, 2, 3000000, time.UTC)
	cfg := Settings{TradingSettings: TradingSettings{RiskPct: 1, Leverage: 2, OKXAPIKey: "SECRET_SENTINEL", OKXAPISecret: "SECRET_SENTINEL", OKXPassphrase: "SECRET_SENTINEL"}}
	return ResearchCaptureInput{
		CaptureID: "11111111-1111-4111-8111-111111111111", ProtocolID: "exit-study-1", Symbol: "ETH-USDT-SWAP", Side: "long", Timeframe: "15m",
		Build: ResearchBuildIdentity{Revision: strings.Repeat("a", 40)}, EntryAt: at,
		Observation: ResearchEntryObservation{Planned: ResearchTradeValues{100, 2, 95, 115, 5}, Metadata: ResearchMetadataObservation{
			RawTickSz: "0.01", RawLotSz: "0.1", RawMinSz: "0.1", RawCtVal: "1", RawCtMult: "1", TickSz: .01, LotSz: .1, MinSz: .1, EffectiveCtVal: 1, Kind: ContractLinearUSDT, SettleCcy: "USDT", CtValCcy: "ETH", ReceivedAt: at.Add(-time.Second)},
			Settings: []ResearchSettingsObservation{ProjectResearchSettings("calc", at, cfg, 1), ProjectResearchSettings("sizing", at, cfg, 1), ProjectResearchSettings("open", at, cfg, 1)}},
		Actual: ResearchTradeValues{101, 2, 95, 115, 6}, Evidence: ResearchEntryEvidence{Status: "reported_complete", TimeSource: "exchange_fills", FirstFillAt: &at, LastFillAt: &at, FilledSize: 2, FillCount: 1, Reasons: []string{}},
	}
}

func TestCaptureWhitelistAndNullOnFailure(t *testing.T) {
	raw, code := BuildResearchEntrySnapshot(captureFixture())
	if code != "" || len(raw) > 32768 {
		t.Fatalf("capture failed: %s", code)
	}
	for _, forbidden := range []string{"SECRET_SENTINEL", "okx_api", "order_id", "telegram_id", "user_id"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("unsafe field: %s", forbidden)
		}
	}
	for _, want := range []string{`"partial_enabled":false`, `"be_offset_r":0`, `"schema":1`, `"capture_status":"complete"`} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("missing %s", want)
		}
	}
	cases := map[string]func(*ResearchCaptureInput){
		"nonfinite": func(i *ResearchCaptureInput) { i.Actual.Entry = math.NaN() },
		"id":        func(i *ResearchCaptureInput) { i.CaptureID = "not-an-id" },
		"protocol":  func(i *ResearchCaptureInput) { i.ProtocolID = "" },
		"oversize": func(i *ResearchCaptureInput) {
			for n := 0; n < 60; n++ {
				i.Observation.Settings = append(i.Observation.Settings, i.Observation.Settings[0])
			}
		},
		"raw_error": func(i *ResearchCaptureInput) { i.Evidence.Reasons = []string{"SECRET_SENTINEL"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := captureFixture()
			mutate(&in)
			raw, code := BuildResearchEntrySnapshot(in)
			if raw != nil || code == "" {
				t.Fatal("invalid capture accepted")
			}
		})
	}
}

func TestCaptureDefaults(t *testing.T) {
	o := ProjectResearchSettings("calc", time.Now(), Settings{}, 0)
	if o.Raw.StaleAfterBars != 0 || o.Effective.StaleAfterBars != 16 || o.Effective.StaleMinMFER != .35 || o.Effective.StaleExitProfitR != .25 || o.Effective.StaleNearBER != -.03 || o.Effective.StaleMaxAdverseR != -.65 || o.Effective.StaleGraceBars != 6 || o.Effective.StaleWorseByR != .30 || o.Effective.StaleTightenToBER != .05 {
		t.Fatalf("bad defaults: %+v", o)
	}
	if o.Raw.PartialEnabled || o.Effective.PartialEnabled {
		t.Fatal("false lost")
	}
}

func TestCaptureCanonicalChecksum(t *testing.T) {
	in := captureFixture()
	first, _ := BuildResearchEntrySnapshot(in)
	second, _ := BuildResearchEntrySnapshot(in)
	if !bytes.Equal(first, second) {
		t.Fatal("nondeterministic")
	}
	var envelope struct {
		Payload  json.RawMessage `json:"payload"`
		Checksum string          `json:"checksum"`
	}
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(envelope.Payload)
	if envelope.Checksum != hex.EncodeToString(sum[:]) {
		t.Fatal("checksum does not cover canonical payload")
	}
	in.Actual.SL = 96
	third, _ := BuildResearchEntrySnapshot(in)
	if bytes.Equal(first, third) {
		t.Fatal("SL not covered")
	}
	// Canonical UTC should not depend on equivalent source location.
	in = captureFixture()
	in.EntryAt = in.EntryAt.In(time.FixedZone("offset", 3*3600))
	third, _ = BuildResearchEntrySnapshot(in)
	if !bytes.Equal(first, third) {
		t.Fatal("timezone-dependent checksum")
	}
}

func TestCaptureSettingsChangeIsIncomplete(t *testing.T) {
	in := captureFixture()
	in.Observation.Settings[1] = ProjectResearchSettings("sizing", in.EntryAt, Settings{TradingSettings: TradingSettings{RiskPct: 2, Leverage: 2}}, 2)
	raw, code := BuildResearchEntrySnapshot(in)
	if code != "" || !bytes.Contains(raw, []byte(`"settings_changed_during_entry"`)) || !bytes.Contains(raw, []byte(`"capture_status":"incomplete"`)) {
		t.Fatal("mixed settings accepted")
	}
	in = captureFixture()
	in.Observation.Metadata.RawTickSz = ""
	raw, code = BuildResearchEntrySnapshot(in)
	if code != "" || !bytes.Contains(raw, []byte(`"metadata_incomplete"`)) {
		t.Fatal("invented missing metadata")
	}
}
