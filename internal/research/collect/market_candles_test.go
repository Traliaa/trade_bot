//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func marketWindow() MarketWindow {
	return MarketWindow{Symbol: "ETH-USDT-SWAP", From: time.Date(2026, 10, 8, 5, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 8, 5, 2, 0, 0, time.UTC), MaxPages: 200}
}
func wireCandle(at time.Time, source string) []string {
	row := []string{strconv.FormatInt(at.UnixMilli(), 10), "100.00", "105", "95", "102"}
	if source == "trade" {
		row = append(row, "0", "0", "0")
	}
	return append(row, "1")
}
func marketResponse(rows any) *http.Response {
	b, _ := json.Marshal(map[string]any{"code": "0", "msg": "", "data": rows})
	return archiveResponse(200, string(b))
}
func TestMarketCandlesGridAndOverlap(t *testing.T) {
	for _, source := range []string{"trade", "mark"} {
		t.Run(source, func(t *testing.T) {
			w := marketWindow()
			pages := 0
			c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
				pages++
				path := "/api/v5/market/history-candles"
				if source == "mark" {
					path = "/api/v5/market/history-mark-price-candles"
				}
				if r.URL.Path != path || r.URL.Query().Get("bar") != "1m" || r.URL.Query().Get("limit") != "100" || r.Header.Get("OK-ACCESS-SIGN") != "" {
					t.Fatal("wrong public request")
				}
				switch pages {
				case 1:
					if r.URL.Query().Get("after") != strconv.FormatInt(w.To.UnixMilli()-1, 10) {
						t.Fatal("wrong initial fencepost")
					}
					return marketResponse([][]string{wireCandle(w.From.Add(time.Minute), source)}), nil
				case 2:
					if r.URL.Query().Get("after") != strconv.FormatInt(w.From.Add(time.Minute).UnixMilli(), 10) {
						t.Fatal("cursor did not advance")
					}
					dup := wireCandle(w.From.Add(time.Minute), source)
					dup[1] = "100.000"
					return marketResponse([][]string{dup, wireCandle(w.From, source)}), nil
				default:
					return marketResponse([][]string{wireCandle(w.From.Add(-time.Minute), source)}), nil
				}
			})
			a, err := c.CollectCandles(context.Background(), w, source)
			if err != nil || a.Coverage.Status != "complete" || !a.Coverage.WindowTraversed || a.Coverage.Pages != 3 || a.ExpectedMinutes != 2 || a.MissingMinutes != 0 || len(a.Candles) != 2 {
				t.Fatalf("coverage %+v %v", a, err)
			}
			if !a.Candles[0].At.Equal(w.From) || a.Candles[0].Prices.Open != "100.00" {
				t.Fatal("unordered/lossy output")
			}
			if source == "trade" && (a.Candles[0].Volume == nil || *a.Candles[0].Volume != "0") {
				t.Fatal("zero volume lost")
			}
			if source == "mark" && a.Candles[0].Volume != nil {
				t.Fatal("invented mark volume")
			}
		})
	}
}
func TestMarketCandlesFullHorizon(t *testing.T) {
	w := marketWindow()
	w.From = w.To.Add(-72 * time.Hour)
	pages := 0
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		pages++
		cursor, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		at := time.UnixMilli(cursor).Add(-time.Millisecond).Truncate(time.Minute)
		rows := [][]string{}
		for i := 0; i < 100; i++ {
			rows = append(rows, wireCandle(at.Add(-time.Duration(i)*time.Minute), "trade"))
		}
		return marketResponse(rows), nil
	})
	a, err := c.CollectCandles(context.Background(), w, "trade")
	if err != nil || a.Coverage.Status != "complete" || len(a.Candles) != 4320 || a.ExpectedMinutes != 4320 || a.MissingMinutes != 0 || pages > 45 {
		t.Fatalf("72h incomplete %+v %v", a.Coverage, err)
	}
}

func TestMarketCandlesLatestClosedBoundary(t *testing.T) {
	w := marketWindow()
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		cursor, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		if cursor > w.To.UnixMilli() {
			// The API is allowed to return the still-open candle at the right
			// boundary if the caller asks for data later than that boundary.
			row := wireCandle(w.To, "trade")
			row[8] = "0"
			return marketResponse([][]string{row, wireCandle(w.From.Add(time.Minute), "trade")}), nil
		}
		return marketResponse([][]string{wireCandle(w.From.Add(time.Minute), "trade"), wireCandle(w.From, "trade"), wireCandle(w.From.Add(-time.Minute), "trade")}), nil
	})
	c.now = func() time.Time { return w.To }
	a, err := c.CollectCandles(context.Background(), w, "trade")
	if err != nil || a.Coverage.Status != "complete" || a.MissingMinutes != 0 {
		t.Fatalf("requested an irrelevant live candle: %+v %v", a.Coverage, err)
	}
}
func TestMarketCandlesEmptyAndGap(t *testing.T) {
	for _, empty := range []bool{false, true} {
		w := marketWindow()
		pages := 0
		c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
			pages++
			if pages == 1 && !empty {
				return marketResponse([][]string{wireCandle(w.From, "trade")}), nil
			}
			return marketResponse([][]string{}), nil
		})
		a, err := c.CollectCandles(context.Background(), w, "trade")
		want := 1
		if empty {
			want = 2
		}
		if err == nil || a.Coverage.Status != "incomplete" || !a.Coverage.WindowTraversed || a.MissingMinutes != want || len(a.Candles) != 2-want {
			t.Fatalf("missing minutes hidden %+v %v", a, err)
		}
	}
}
func TestMarketCandlesRejectsBadPages(t *testing.T) {
	for _, kind := range []string{"short_row", "extra_field", "unclosed", "bad_decimal", "bad_geometry", "negative_volume", "unaligned", "too_many", "alias", "conflict", "repeat", "newer"} {
		t.Run(kind, func(t *testing.T) {
			w := marketWindow()
			pages := 0
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				pages++
				row := wireCandle(w.From.Add(time.Minute), "trade")
				switch kind {
				case "short_row":
					row = row[:8]
				case "extra_field":
					row = append(row, "junk")
				case "unclosed":
					row[8] = "0"
				case "bad_decimal":
					row[2] = "NaN"
				case "bad_geometry":
					row[2] = "99"
				case "negative_volume":
					row[5] = "-1"
				case "unaligned":
					row[0] = strconv.FormatInt(w.From.UnixMilli()+1, 10)
				case "alias":
					return archiveResponse(200, `{"code":"0","DATA":[]}`), nil
				case "too_many":
					rows := make([][]string, 101)
					for i := range rows {
						rows[i] = row
					}
					return marketResponse(rows), nil
				case "conflict":
					if pages == 2 {
						row[1] = "101"
					}
				case "newer":
					if pages == 2 {
						row = wireCandle(w.To, "trade")
					}
				}
				return marketResponse([][]string{row}), nil
			})
			a, err := c.CollectCandles(context.Background(), w, "trade")
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.WindowTraversed || pages > 2 {
				t.Fatalf("bad page accepted %+v %v", a.Coverage, err)
			}
			if kind != "conflict" && kind != "repeat" && kind != "newer" && len(a.Candles) != 0 {
				t.Fatal("invalid page partially accepted")
			}
		})
	}
}
func TestMarketCandlesScope(t *testing.T) {
	for _, kind := range []string{"future", "old", "wide", "subminute", "empty", "symbol", "source", "zero_pages", "many_pages", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) { calls++; return marketResponse([][]string{}), nil })
			w := marketWindow()
			source := "trade"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "future":
				w.To = w.To.Add(24 * time.Hour)
			case "old":
				w.From = w.From.Add(-81 * 24 * time.Hour)
				w.To = w.From.Add(time.Hour)
			case "wide":
				w.From = w.To.Add(-73 * time.Hour)
			case "subminute":
				w.From = w.From.Add(time.Millisecond)
			case "empty":
				w.From = w.To
			case "symbol":
				w.Symbol = "SECRET-invalid"
			case "source":
				source = "https://other.invalid"
			case "zero_pages":
				w.MaxPages = 0
			case "many_pages":
				w.MaxPages = 201
			case "cancel":
				cancel()
			}
			a, err := c.CollectCandles(ctx, w, source)
			if err == nil || calls != 0 || a.Coverage.Status != "pending" || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("invalid scope reached network", err)
			}
		})
	}
}
func TestMarketCandlesCancellationQuotaBudget(t *testing.T) {
	for _, kind := range []string{"cancel", "quota", "budget"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := marketWindow()
			w.From = w.To.Add(-72 * time.Hour)
			pages := 0
			c, dir := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				pages++
				if kind == "cancel" {
					cancel()
					return marketResponse([][]string{}), nil
				}
				return marketResponse([][]string{wireCandle(w.To.Add(-time.Duration(pages)*time.Minute), "trade")}), nil
			})
			if kind == "quota" {
				var b strings.Builder
				for i := 1; i < dailyRequests; i++ {
					fmt.Fprintf(&b, "%d\n", i)
				}
				if err := os.WriteFile(filepath.Join(dir, ".quota-2026-10-08"), []byte(b.String()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := c.CollectCandles(ctx, w, "trade")
			wantPages := 1
			if kind == "budget" {
				wantPages = 200
			}
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.WindowTraversed || pages != wantPages {
				t.Fatalf("interrupted traversal complete %+v %v", a.Coverage, err)
			}
		})
	}
}
