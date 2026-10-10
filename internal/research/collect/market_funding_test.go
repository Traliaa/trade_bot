//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func wireFunding(at time.Time, realized string) map[string]string {
	return map[string]string{"instId": "ETH-USDT-SWAP", "instType": "SWAP", "fundingTime": strconv.FormatInt(at.UnixMilli(), 10), "fundingRate": "0.8", "realizedRate": realized, "method": "next_period", "formulaType": "withRate", "ignored": "NOT-FOR-EXPORT"}
}
func TestMarketFundingIrregularTimesAndRealizedRates(t *testing.T) {
	w := marketWindow()
	w.From = w.From.Add(123 * time.Millisecond)
	pages := 0
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		pages++
		q := r.URL.Query()
		if r.URL.Path != "/api/v5/public/funding-rate-history" || q.Get("bar") != "" || q.Get("limit") != "100" || r.Header.Get("OK-ACCESS-KEY") != "" {
			t.Fatal("wrong funding endpoint/query/auth")
		}
		switch pages {
		case 1:
			return marketResponse([]map[string]string{wireFunding(w.To, "0.5"), wireFunding(w.To.Add(-time.Millisecond), "-0.0010")}), nil
		case 2:
			if q.Get("after") != strconv.FormatInt(w.To.Add(-time.Millisecond).UnixMilli(), 10) {
				t.Fatal("wrong funding cursor")
			}
			return marketResponse([]map[string]string{wireFunding(w.To.Add(-time.Millisecond), "-0.001"), wireFunding(w.From, "0.0000")}), nil
		default:
			return marketResponse([]map[string]string{wireFunding(w.From.Add(-time.Millisecond), "0.6")}), nil
		}
	})
	a, err := c.CollectFunding(context.Background(), w)
	if err != nil || a.Coverage.Status != "complete" || !a.Coverage.WindowTraversed || a.Coverage.Pages != 3 || a.Coverage.Records != 2 || len(a.Rates) != 2 {
		t.Fatalf("bad coverage %+v %v", a, err)
	}
	if a.ScheduleVerified || a.Costs != "sensitivity-only" || !a.Rates[0].At.Equal(w.From) || a.Rates[0].RealizedRate != "0.0000" || a.Rates[1].RealizedRate != "-0.0010" {
		t.Fatal("invented costs, cadence or forecast substitution", a)
	}
	b, err := json.Marshal(a)
	if err != nil || strings.Contains(string(b), "NOT-FOR-EXPORT") || strings.Contains(string(b), "0.8") || strings.Contains(string(b), "fee_usdt") {
		t.Fatal("non-whitelisted funding data serialized")
	}
}
func TestMarketFundingEmptyIsNotZeroCost(t *testing.T) {
	c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) { return marketResponse([]map[string]string{}), nil })
	a, err := c.CollectFunding(context.Background(), marketWindow())
	if err != nil || !a.Coverage.WindowTraversed || a.ScheduleVerified || a.Costs != "unknown" || len(a.Rates) != 0 {
		t.Fatal("empty archive certified funding=0", a, err)
	}
}
func TestMarketFundingBadPages(t *testing.T) {
	for _, kind := range []string{"missing_realized", "forecast_only", "malformed", "missing_time", "wrong_symbol", "wrong_type", "case_alias", "envelope_alias", "conflict", "repeat", "newer", "oversized", "null"} {
		t.Run(kind, func(t *testing.T) {
			w := marketWindow()
			pages := 0
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				pages++
				row := wireFunding(w.From, "0.01")
				switch kind {
				case "missing_realized":
					delete(row, "realizedRate")
				case "forecast_only":
					row["realizedRate"] = ""
				case "malformed":
					row["realizedRate"] = "1e-6"
				case "missing_time":
					delete(row, "fundingTime")
				case "wrong_symbol":
					row["instId"] = "BTC-USDT-SWAP"
				case "wrong_type":
					row["instType"] = "FUTURES"
				case "case_alias":
					row["REALIZEDRATE"] = "0.02"
				case "envelope_alias":
					return archiveResponse(200, `{"code":"0","DATA":[]}`), nil
				case "conflict":
					if pages > 1 {
						row["realizedRate"] = "0.02"
					}
				case "newer":
					if pages > 1 {
						row = wireFunding(w.To, "0.01")
					}
				case "oversized":
					rows := make([]map[string]string, 101)
					for i := range rows {
						rows[i] = row
					}
					return marketResponse(rows), nil
				case "null":
					return archiveResponse(200, `{"code":"0","data":[null]}`), nil
				}
				return marketResponse([]map[string]string{row}), nil
			})
			a, err := c.CollectFunding(context.Background(), w)
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.WindowTraversed || a.ScheduleVerified || a.Costs == "verified" || pages > 2 {
				t.Fatalf("bad funding accepted %+v %v", a, err)
			}
		})
	}
}
func TestMarketFundingCancellationAndBudget(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		w := marketWindow()
		w.MaxPages = 1
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
			if cancelled {
				cancel()
				return marketResponse([]map[string]string{}), nil
			}
			return marketResponse([]map[string]string{wireFunding(w.From, "0")}), nil
		})
		a, err := c.CollectFunding(ctx, w)
		if err == nil || a.Coverage.WindowTraversed || a.Coverage.Status != "incomplete" || a.Coverage.Pages != 1 || a.ScheduleVerified {
			t.Fatal("interrupted funding completed", a, err)
		}
	}
}
