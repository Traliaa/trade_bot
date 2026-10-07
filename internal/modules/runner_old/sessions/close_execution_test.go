package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/models"
	okx "trade_bot/internal/modules/okx_client/service"
)

// These are synthetic IDs with the four sizes/prices observed in the ARB audit.
func arbCloseRows() []map[string]string {
	rows := make([]map[string]string, 0, 4)
	for i, size := range []string{"0.4", "0.9", "0.4", "0.1"} {
		rows = append(rows, map[string]string{
			"instId": "ARB-USDT-SWAP", "posSide": "long", "side": "sell",
			"ordId": "fixture-close", "tradeId": fmt.Sprint(i), "fillSz": size,
			"fillPx": "0.20451", "fee": "-0.001", "fillPnl": "-0.01",
			"ts": fmt.Sprint(1791195362291 + i/2), "fillTime": "1791195362291",
		})
	}
	return rows
}

func appendChangedCloseRow(rows []map[string]string, key, value string) []map[string]string {
	other := make(map[string]string)
	for k, v := range rows[0] {
		other[k] = v
	}
	other[key] = value
	return append(rows, other)
}

func closeFixtureClient(t *testing.T, rows []map[string]string) *okx.Client {
	t.Helper()
	body, err := json.Marshal(map[string]any{"code": "0", "data": rows})
	if err != nil {
		t.Fatal(err)
	}
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v5/trade/fills" {
			t.Fatalf("unexpected exchange request: %s %s", r.Method, r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	return okx.NewClient(&models.UserSettings{Settings: models.Settings{TradingSettings: models.TradingSettings{OKXAPIKey: "fixture", OKXAPISecret: "fixture", OKXPassphrase: "fixture"}}})
}

func TestCloseExecutionExactVolume(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func([]map[string]string) []map[string]string
		wantErr bool
	}{
		{"arb", func(r []map[string]string) []map[string]string { return r }, false},
		{"duplicate", func(r []map[string]string) []map[string]string { return append(r, r[0]) }, false},
		{"equivalent_decimal_duplicate", func(r []map[string]string) []map[string]string { return appendChangedCloseRow(r, "fillSz", "0.40") }, false},
		{"conflicting_duplicate", func(r []map[string]string) []map[string]string { r[1]["tradeId"] = r[0]["tradeId"]; return r }, true},
		{"conflict_hidden_by_side_filter", func(r []map[string]string) []map[string]string { return appendChangedCloseRow(r, "side", "buy") }, true},
		{"conflict_hidden_by_position_filter", func(r []map[string]string) []map[string]string { return appendChangedCloseRow(r, "posSide", "short") }, true},
		{"conflict_hidden_by_time_filter", func(r []map[string]string) []map[string]string {
			return appendChangedCloseRow(r, "ts", "1791184503029")
		}, true},
		{"unrelated_instrument_id", func(r []map[string]string) []map[string]string {
			return appendChangedCloseRow(r, "instId", "ETH-USDT-SWAP")
		}, false},
		{"real_excess", func(r []map[string]string) []map[string]string { r[3]["fillSz"] = "0.2"; return r }, true},
		// This exceeds entry in decimal but rounds to the same float. No epsilon may hide it.
		{"sub_float_excess", func(r []map[string]string) []map[string]string { r[3]["fillSz"] = "0.100000000000000001"; return r }, true},
		{"missing_size", func(r []map[string]string) []map[string]string { r[0]["fillSz"] = ""; return r }, true},
		{"negative_size", func(r []map[string]string) []map[string]string { r[0]["fillSz"] = "-0.4"; return r }, true},
		{"fraction_not_decimal", func(r []map[string]string) []map[string]string { r[0]["fillSz"] = "2/5"; return r }, true},
		{"oversized_decimal", func(r []map[string]string) []map[string]string {
			r[0]["fillSz"] = "0." + strings.Repeat("1", 127)
			return r
		}, true},
		{"zero_size", func(r []map[string]string) []map[string]string { r[0]["fillSz"] = "0"; return r }, true},
		{"missing_order", func(r []map[string]string) []map[string]string { r[0]["ordId"] = ""; return r }, true},
		{"missing_id", func(r []map[string]string) []map[string]string { r[0]["tradeId"] = ""; return r }, true},
		{"invalid_fee", func(r []map[string]string) []map[string]string { r[0]["fee"] = "NaN"; return r }, true},
		{"invalid_price", func(r []map[string]string) []map[string]string { r[0]["fillPx"] = "0"; return r }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &UserSession{Okx: closeFixtureClient(t, tc.mutate(arbCloseRows()))}
			tr := models.TradeRecord{InstID: "ARB-USDT-SWAP", EntryAt: time.UnixMilli(1791184503030), Payload: models.TradePayload{PosSide: "long", EntrySize: 1.8}}
			got, err := s.resolveClosedTradeExecution(context.Background(), tr)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("unsafe executions accepted: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.ExitSize != 1.8 || len(got.Fills) != 4 || math.Abs(got.ExitPrice-.20451) > 1e-15 || got.TotalFees != -.004 || got.GrossRealizedPnL != -.04 {
				t.Fatalf("incorrect close accounting: %+v", got)
			}
		})
	}
}

func TestCloseExecutionRejectsUnknownEntry(t *testing.T) {
	for _, entry := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		s := &UserSession{Okx: closeFixtureClient(t, arbCloseRows())}
		tr := models.TradeRecord{InstID: "ARB-USDT-SWAP", EntryAt: time.UnixMilli(1791184503030), Payload: models.TradePayload{PosSide: "long", EntrySize: entry}}
		if _, err := s.resolveClosedTradeExecution(context.Background(), tr); err == nil {
			t.Fatal("unknown entry accepted")
		}
	}
	if _, err := aggregateCloseExecution(nil, 1.8); err == nil {
		t.Fatal("nil fills accepted")
	}
}

func TestCloseExecutionDecimalLeadingZeros(t *testing.T) {
	rows := arbCloseRows()[:1]
	rows[0]["fillSz"] = "010"
	s := &UserSession{Okx: closeFixtureClient(t, rows)}
	tr := models.TradeRecord{InstID: "ARB-USDT-SWAP", EntryAt: time.UnixMilli(1791184503030), Payload: models.TradePayload{PosSide: "long", EntrySize: 10}}
	got, err := s.resolveClosedTradeExecution(context.Background(), tr)
	if err != nil || got.ExitSize != 10 {
		t.Fatalf("decimal quantity interpreted as another base: size=%v err=%v", got.ExitSize, err)
	}
}
