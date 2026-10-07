package service

import (
	"bytes"
	"context"
	"errors"
	"math"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
)

func researchServiceFixture() (models.TradeRecord, *models.TradeParams, *models.OpenResult) {
	at := time.Date(2026, 10, 7, 8, 1, 0, 0, time.UTC)
	settings := []models.ResearchSettingsObservation{}
	for _, stage := range []string{"calc", "sizing", "open"} {
		settings = append(settings, models.ProjectResearchSettings(stage, at, models.Settings{}, 0))
	}
	tr := models.TradeRecord{InstID: "ETH-USDT-SWAP", Timeframe: "15m", EntryAt: at, Payload: models.TradePayload{PosSide: "long", EntryPrice: 101, EntrySize: 2}}
	p := &models.TradeParams{Entry: 101, Size: 2, SL: 95, TP: 115, RiskDist: 6, ResearchEntry: &models.ResearchEntryObservation{
		Planned: models.ResearchTradeValues{Entry: 100, Size: 2, SL: 95, TP: 115, RiskDist: 5}, Settings: settings,
		Metadata: models.ResearchMetadataObservation{RawTickSz: "0.1", RawLotSz: "1", RawMinSz: "1", RawCtVal: "1", RawCtMult: "1", TickSz: .1, LotSz: 1, MinSz: 1, EffectiveCtVal: 1, Kind: models.ContractLinearUSDT, SettleCcy: "USDT", CtValCcy: "ETH", ReceivedAt: at},
	}}
	o := &models.OpenResult{PosSide: "long", Entry: 101, EntryAt: at, ResearchEvidence: &models.ResearchEntryEvidence{Status: "reported_complete", TimeSource: "exchange_fills", FirstFillAt: &at, LastFillAt: &at, FilledSize: 2, FillCount: 1}}
	return tr, p, o
}
func researchTestID() (string, error) { return "11111111-1111-4111-8111-111111111111", nil }

func TestPrepareResearchCaptureDisabled(t *testing.T) {
	raw, code := prepareResearchEntrySnapshot(config.ResearchCaptureConfig{}, models.TradeRecord{}, nil, nil, models.ResearchBuildIdentity{}, func() (string, error) { t.Fatal("generated disabled ID"); return "", nil })
	if raw != nil || code != "" {
		t.Fatal("disabled capture did work")
	}
}

func TestPrepareResearchCaptureFailureKeepsTradeInsert(t *testing.T) {
	for _, mode := range []string{"uuid", "nonfinite", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			tr, p, o := researchServiceFixture()
			want := tr.Payload
			calls := 0
			dbErr := errors.New("db failure")
			r := researchEntryRecorder{cfg: config.ResearchCaptureConfig{Enabled: true, ProtocolID: "test"}, build: models.ResearchBuildIdentity{Revision: strings.Repeat("a", 40)}, newID: researchTestID}
			if mode == "uuid" {
				r.newID = func() (string, error) { return "", errors.New("PRIVATE_ERROR") }
			}
			if mode == "nonfinite" {
				p.Entry = math.NaN()
			}
			if mode == "oversize" {
				for n := 0; n < 60; n++ {
					p.ResearchEntry.Settings = append(p.ResearchEntry.Settings, p.ResearchEntry.Settings[0])
				}
			}
			stats := map[string]int{}
			r.count = func(s string) { stats[s]++ }
			r.create = func(_ context.Context, got models.TradeRecord) error {
				calls++
				if got.ResearchEntrySnapshot != nil || !reflect.DeepEqual(got.Payload, want) {
					t.Fatal("capture changed saved trade")
				}
				return dbErr
			}
			if err := r.save(context.Background(), tr, p, o); !errors.Is(err, dbErr) || calls != 1 || stats["research_capture_failed"] != 1 {
				t.Fatal("capture swallowed or blocked DB operation")
			}
		})
	}
}

func TestCaptureOnOffServiceParity(t *testing.T) {
	tr, p, o := researchServiceFixture()
	var saved []models.TradeRecord
	for _, enabled := range []bool{false, true} {
		r := researchEntryRecorder{cfg: config.ResearchCaptureConfig{Enabled: enabled, ProtocolID: "test"}, build: models.ResearchBuildIdentity{Revision: strings.Repeat("a", 40)}, newID: researchTestID, count: func(string) {}, create: func(_ context.Context, tr models.TradeRecord) error { saved = append(saved, tr); return nil }}
		if err := r.save(context.Background(), tr, p, o); err != nil {
			t.Fatal(err)
		}
	}
	if saved[0].ResearchEntrySnapshot != nil || len(saved[1].ResearchEntrySnapshot) == 0 {
		t.Fatal("wrong capture gating")
	}
	saved[1].ResearchEntrySnapshot = nil
	if !reflect.DeepEqual(saved[0], saved[1]) {
		t.Fatal("capture changed nonresearch fields")
	}
}

func TestCaptureBuildUnknown(t *testing.T) {
	build := researchBuildIdentityFromInfo(nil)
	if !build.Unknown || build.Revision != "" {
		t.Fatal("invented revision")
	}
	build = researchBuildIdentityFromInfo(&debug.BuildInfo{Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: strings.Repeat("a", 40)}, {Key: "vcs.modified", Value: "true"}}})
	if build.Unknown || !build.Dirty {
		t.Fatal("lost dirty identity")
	}
	tr, p, o := researchServiceFixture()
	raw, code := prepareResearchEntrySnapshot(config.ResearchCaptureConfig{Enabled: true, ProtocolID: "test"}, tr, p, o, models.ResearchBuildIdentity{Unknown: true}, researchTestID)
	if code != "" || !bytes.Contains(raw, []byte(`"build_unknown"`)) {
		t.Fatal("unknown build hidden")
	}
}
