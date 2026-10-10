package collect

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"trade_bot/internal/models"
)

func snapshotFixture(t *testing.T, incomplete bool) []byte {
	t.Helper()
	at := time.Date(2026, 10, 8, 10, 1, 2, 3000000, time.UTC)
	cfg := models.Settings{}
	in := models.ResearchCaptureInput{
		CaptureID: "11111111-1111-4111-8111-111111111111", ProtocolID: "study", Symbol: "ETH-USDT-SWAP", Side: "long", Timeframe: "15m",
		EntryAt: at, Build: models.ResearchBuildIdentity{Revision: strings.Repeat("a", 40)},
		Observation: models.ResearchEntryObservation{
			Planned:  models.ResearchTradeValues{Entry: 100, Size: 2, SL: 95, TP: 115, RiskDist: 5},
			Metadata: models.ResearchMetadataObservation{RawTickSz: "0.01", RawLotSz: "0.1", RawMinSz: "0.1", RawCtVal: "1", RawCtMult: "1", TickSz: .01, LotSz: .1, MinSz: .1, EffectiveCtVal: 1, Kind: models.ContractLinearUSDT, SettleCcy: "USDT", CtValCcy: "ETH", ReceivedAt: at},
			Settings: []models.ResearchSettingsObservation{models.ProjectResearchSettings("calc", at, cfg, 1), models.ProjectResearchSettings("sizing", at, cfg, 1), models.ProjectResearchSettings("open", at, cfg, 1)},
		},
		Actual:   models.ResearchTradeValues{Entry: 100, Size: 2, SL: 95, TP: 115, RiskDist: 5},
		Evidence: models.ResearchEntryEvidence{Status: "reported_complete", TimeSource: "exchange_fills", FirstFillAt: &at, LastFillAt: &at, FilledSize: 2, FillCount: 1, Reasons: []string{}},
	}
	if incomplete {
		in.Build.Unknown = true
	}
	raw, code := models.BuildResearchEntrySnapshot(in)
	if code != "" {
		t.Fatal(code)
	}
	return raw
}

func TestSnapshotJSONB(t *testing.T) {
	raw := snapshotFixture(t, false)
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	reordered, _ := json.MarshalIndent(fields, "", "  ")
	in, status, err := DecodeSnapshot(reordered, "study")
	if err != nil || status != "complete" || in.Actual.Size != 2 {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func TestSnapshotRejectsAlteration(t *testing.T) {
	raw := string(snapshotFixture(t, false))
	cases := map[string]string{
		"amount":           strings.Replace(raw, `"size":2`, `"size":3`, 1),
		"sub_float_amount": strings.Replace(raw, `"size":2`, `"size":2.00000000000000001`, 1),
		"unknown":          strings.Replace(raw, `"schema":1`, `"private_secret":"SECRET","schema":1`, 1),
		"missing":          strings.Replace(raw, `"dirty":false,`, "", 1),
		"duplicate":        strings.Replace(raw, `"schema":1`, `"schema":1,"schema":1`, 1),
		"oversize":         strings.Repeat(" ", 32769),
		"trailing":         raw + `{}`,
	}
	for name, value := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := DecodeSnapshot([]byte(value), "study")
			if err == nil || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("unsafe or missing validation error")
			}
		})
	}
	if _, _, err := DecodeSnapshot([]byte(raw), "other"); err == nil {
		t.Fatal("wrong protocol accepted")
	}
}

func TestSnapshotNumericFormatting(t *testing.T) {
	raw := string(snapshotFixture(t, false))
	for _, number := range []string{"2.0", "2e0", "200e-2"} {
		if _, _, err := DecodeSnapshot([]byte(strings.Replace(raw, `"size":2`, `"size":`+number, 1)), "study"); err != nil {
			t.Fatalf("equivalent number %s rejected: %v", number, err)
		}
	}
}

func TestJSONNumberBounds(t *testing.T) {
	for _, raw := range []string{"1e999999999", "0e-999999999", strings.Repeat("0", 1025)} {
		if _, ok := jsonRational(json.Number(raw)); ok {
			t.Fatal("unbounded number accepted")
		}
	}
	if !sameJSON(json.Number("5e-324"), json.Number("0."+strings.Repeat("0", 323)+"5")) {
		t.Fatal("subnormal decimal expansion rejected")
	}
}

func TestSnapshotIncomplete(t *testing.T) {
	_, status, err := DecodeSnapshot(snapshotFixture(t, true), "study")
	if err != nil || status != "incomplete" {
		t.Fatalf("incomplete promoted: %s %v", status, err)
	}
}
