package sessions

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okx "trade_bot/internal/modules/okx_client/service"
)

func TestCaptureFillEvidence(t *testing.T) {
	at := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	good := models.TradeFill{FillPx: 101, FillSz: 2, FillTime: at}
	for _, tc := range []struct {
		name  string
		fills []models.TradeFill
		err   error
		want  string
	}{
		{"single", []models.TradeFill{good}, nil, "reported_complete"},
		{"timeout", nil, errors.New("PRIVATE_ERROR"), "incomplete"},
		{"empty", nil, nil, "incomplete"},
		{"shortfall", []models.TradeFill{{FillPx: 101, FillSz: 1, FillTime: at}}, nil, "incomplete"},
		{"invalid", []models.TradeFill{{FillPx: 0, FillSz: 2, FillTime: at}}, nil, "incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := researchFillEvidence(tc.fills, 2, tc.err)
			if e.Status != tc.want {
				t.Fatalf("unexpected status: %+v", e)
			}
			for _, r := range e.Reasons {
				if strings.Contains(r, "PRIVATE") {
					t.Fatal("error leaked")
				}
			}
		})
	}
	fills := []models.TradeFill{{FillPx: 101, FillSz: .1, FillTime: at}, {FillPx: 101, FillSz: 1.1, FillTime: at}, {FillPx: 101, FillSz: .6, FillTime: at.Add(time.Millisecond)}}
	e := researchFillEvidence(fills, 1.8, nil)
	if e.FilledSize <= 1.8 || e.Status != "incomplete" || e.FirstFillAt.Equal(*e.LastFillAt) {
		t.Fatalf("ambiguous volume/time hidden: %+v", e)
	}
}

// HTTP transport is fake; parameter calculation, sizing and observations are real.
func TestCaptureNoAdditionalSettingsRead(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	user := &models.UserSettings{}
	s := &UserSession{Config: &config.Config{ResearchCapture: config.ResearchCaptureConfig{Enabled: true}}, User: user, Okx: okx.NewClient(user)}
	cfg := models.Settings{TradingSettings: models.TradingSettings{RiskPct: 1, Leverage: 2, StopPct: 5, TakeProfitRR: 3}}
	s.InitSettings(cfg)
	calls := 0
	http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"code":"0","data":[{"last":"100"}]}`
		if r.URL.Path == "/api/v5/public/instruments" {
			cfg.TradingSettings.RiskPct = 2
			s.UpdateSettings(cfg)
			body = `{"code":"0","data":[{"instId":"ETH-USDT-SWAP","ctType":"linear","settleCcy":"USDT","ctValCcy":"ETH","lotSz":"0.1","minSz":"0.1","tickSz":"0.01","ctVal":"1","ctMult":"1"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	p, err := s.CalcTradeParams(context.Background(), "ETH-USDT-SWAP", "BUY", 100)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || p.ResearchEntry == nil || len(p.ResearchEntry.Settings) != 2 {
		t.Fatal("missing observation or extra I/O")
	}
	if p.ResearchEntry.Settings[0].RiskPct != 1 || p.ResearchEntry.Settings[1].RiskPct != 2 {
		t.Fatal("observed wrong settings read")
	}
	if p.Size != .4 || p.RiskPct != 1 {
		t.Fatalf("changed original math: %+v", p)
	}
	// Observe off using the same actual starting settings and inputs.
	s.Config.ResearchCapture.Enabled = false
	cfg.TradingSettings.RiskPct = 1
	s.UpdateSettings(cfg)
	off, err := s.CalcTradeParams(context.Background(), "ETH-USDT-SWAP", "BUY", 100)
	if err != nil {
		t.Fatal(err)
	}
	p.ResearchEntry = nil
	p.SizeMeta.ResearchSettings = nil
	if !reflect.DeepEqual(p, off) {
		t.Fatal("capture changed parameters")
	}
}

func TestCapturePreservesPlannedBeforeFillMutation(t *testing.T) {
	s := &UserSession{Config: &config.Config{ResearchCapture: config.ResearchCaptureConfig{Enabled: true}}}
	p := &models.TradeParams{Entry: 100, Size: 2, SL: 95, TP: 115, RiskDist: 5}
	o := models.ProjectResearchSettings("calc", time.Now(), models.Settings{}, 1)
	p.ResearchEntry = s.researchEntryObservation(p, models.Instrument{}, &o)
	p.Entry = 101
	p.Size = 1.5
	p.RiskDist = 6
	if p.ResearchEntry.Planned.Entry != 100 || p.ResearchEntry.Planned.Size != 2 || p.ResearchEntry.Planned.RiskDist != 5 {
		t.Fatal("planned values alias mutated params")
	}
}
