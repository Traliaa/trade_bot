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

func boundaryWindow() MarketWindow {
	w := marketWindow()
	w.From = w.From.Add(30123 * time.Millisecond)
	w.To = w.From.Truncate(time.Minute).Add(time.Minute)
	return w
}

func wirePublicTrade(id string, at time.Time, price string) map[string]string {
	return map[string]string{"instId": "ETH-USDT-SWAP", "tradeId": id, "ts": strconv.FormatInt(at.UnixMilli(), 10), "px": price, "sz": "0.0100", "side": "buy", "source": "0", "extra": "NOT-FOR-EXPORT"}
}

// Catches borrowing pre-entry/post-end prices, ending on short pages, lossy
// decimals and duplicate counting. Only the external HTTP transport is fake.
func TestBoundaryArchiveExactWindowAndOverlaps(t *testing.T) {
	w := boundaryWindow()
	pages := 0
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		pages++
		q := r.URL.Query()
		if r.Method != "GET" || r.URL.Path != "/api/v5/market/history-trades" || q.Get("instId") != w.Symbol || q.Get("limit") != "100" || q.Get("before") != "" || r.Header.Get("OK-ACCESS-KEY") != "" {
			t.Fatal("incorrect public trades request")
		}
		switch pages {
		case 1:
			if q.Get("type") != "2" || q.Get("after") != strconv.FormatInt(w.To.UnixMilli(), 10) {
				t.Fatal("incorrect timestamp seek")
			}
			return marketResponse([]map[string]string{wirePublicTrade("12", w.To, "999"), wirePublicTrade("11", w.To.Add(-time.Millisecond), "105.00")}), nil
		case 2:
			if q.Get("type") != "1" || q.Get("after") != "11" {
				t.Fatal("did not switch to ID paging")
			}
			return marketResponse([]map[string]string{wirePublicTrade("11", w.To.Add(-time.Millisecond), "105"), wirePublicTrade("10", w.From, "100.00")}), nil
		case 3:
			if q.Get("after") != "10" {
				t.Fatal("wrong ID cursor")
			}
			return marketResponse([]map[string]string{wirePublicTrade("9", w.From.Add(-time.Millisecond), "1")}), nil
		default:
			t.Fatal("unbounded traversal")
			return nil, nil
		}
	})
	a, err := c.CollectBoundaryTrades(context.Background(), w)
	if err != nil || a.Coverage.Status != "complete" || !a.Coverage.WindowTraversed || a.Coverage.Pages != 3 || a.Coverage.Records != 2 || len(a.Trades) != 2 {
		t.Fatalf("wrong archive %+v %v", a, err)
	}
	if a.Segment.Kind != "boundary" || a.Report.Coverage != "complete" || a.Report.Trades != 2 || a.Report.Prices == nil || *a.Report.Prices != (OHLC{"100", "105", "100", "105"}) || a.Trades[0].ID != "10" || a.Trades[1].Price != "105.00" || a.Trades[0].Size != "0.0100" {
		t.Fatalf("wrong boundary evidence %+v", a)
	}
	b, err := json.Marshal(a)
	if err != nil || strings.Contains(string(b), "NOT-FOR-EXPORT") || !strings.Contains(string(b), `"trade_id":"10"`) {
		t.Fatal("unsafe/lossy export")
	}
}

func TestBoundaryArchiveSaturatedTimestamp(t *testing.T) {
	w := boundaryWindow()
	pages := 0
	c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
		pages++
		rows := []map[string]string{}
		switch pages {
		case 1:
			for i := 250; i >= 151; i-- {
				rows = append(rows, wirePublicTrade(strconv.Itoa(i), w.From, "100"))
			}
		case 2:
			if r.URL.Query().Get("type") != "1" || r.URL.Query().Get("after") != "151" {
				t.Fatal("time tie skipped")
			}
			for i := 150; i >= 101; i-- {
				rows = append(rows, wirePublicTrade(strconv.Itoa(i), w.From, "100"))
			}
		case 3:
			if r.URL.Query().Get("after") != "101" {
				t.Fatal("wrong final cursor")
			}
		default:
			t.Fatal("over-read empty terminal")
		}
		return marketResponse(rows), nil
	})
	a, err := c.CollectBoundaryTrades(context.Background(), w)
	if err != nil || !a.Coverage.WindowTraversed || len(a.Trades) != 150 || a.Report.Trades != 150 || a.Report.Prices == nil || a.Trades[0].ID != "101" || a.Trades[149].ID != "250" {
		t.Fatalf("saturated timestamp incomplete %+v %v", a.Coverage, err)
	}
}

func TestBoundaryArchiveAmbiguousOrderAndEmptyPrices(t *testing.T) {
	for _, kind := range []string{"ambiguous", "empty", "outside", "last_boundary", "large_id"} {
		t.Run(kind, func(t *testing.T) {
			w := boundaryWindow()
			if kind == "last_boundary" {
				w.From = w.From.Truncate(time.Minute)
				w.To = w.From.Add(30123 * time.Millisecond)
			}
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				rows := []map[string]string{}
				switch kind {
				case "ambiguous":
					rows = append(rows, wirePublicTrade("12", w.From, "101"), wirePublicTrade("11", w.From, "100"))
				case "last_boundary":
					rows = append(rows, wirePublicTrade("12", w.To, "999"), wirePublicTrade("11", w.From, "100"))
				case "large_id":
					rows = append(rows, wirePublicTrade(strings.Repeat("9", 64), w.From, "100"))
				}
				if kind != "empty" {
					rows = append(rows, wirePublicTrade("1", w.From.Add(-time.Millisecond), "90"))
				}
				return marketResponse(rows), nil
			})
			a, err := c.CollectBoundaryTrades(context.Background(), w)
			if !a.Coverage.WindowTraversed {
				t.Fatalf("window not traversed %+v %v", a, err)
			}
			switch kind {
			case "ambiguous":
				if err == nil || a.Coverage.Status != "incomplete" || a.Report.Coverage != "incomplete" || a.Report.Prices != nil || len(a.Trades) != 2 || !strings.Contains(strings.Join(a.Report.Reasons, ","), "tick_order_ambiguous") {
					t.Fatal("invented tick order", a, err)
				}
			case "empty", "outside":
				if err != nil || a.Coverage.Status != "complete" || a.Report.Prices != nil || len(a.Trades) != 0 {
					t.Fatal("invented prices", a, err)
				}
			default:
				if err != nil || a.Coverage.Status != "complete" || a.Report.Prices == nil || a.Report.Prices.Close != "100" || len(a.Trades) != 1 {
					t.Fatal("boundary/ID precision lost", a, err)
				}
			}
		})
	}
}

func TestBoundaryArchiveRejectsMalformedPageAtomically(t *testing.T) {
	for _, kind := range []string{"id_zero", "id_leading_zero", "id_huge", "id_negative", "bad_price", "zero_price", "size_zero", "side", "source", "missing_source", "symbol", "time", "future", "alias", "envelope_alias", "null", "oversized", "duplicate", "time_order", "newer_than_seek"} {
		t.Run(kind, func(t *testing.T) {
			w := boundaryWindow()
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				row := wirePublicTrade("10", w.From, "100")
				switch kind {
				case "id_zero":
					row["tradeId"] = "0"
				case "id_leading_zero":
					row["tradeId"] = "010"
				case "id_huge":
					row["tradeId"] = strings.Repeat("9", 65)
				case "id_negative":
					row["tradeId"] = "-1"
				case "bad_price":
					row["px"] = "1e2"
				case "zero_price":
					row["px"] = "0"
				case "size_zero":
					row["sz"] = "0"
				case "side":
					row["side"] = "unknown"
				case "source":
					row["source"] = "2"
				case "missing_source":
					delete(row, "source")
				case "symbol":
					row["instId"] = "OTHER-USDT-SWAP"
				case "time":
					row["ts"] = "01"
				case "future":
					row["ts"] = strconv.FormatInt(w.To.Add(24*time.Hour).UnixMilli(), 10)
				case "alias":
					row["PX"] = "200"
				case "envelope_alias":
					return archiveResponse(200, `{"code":"0","DATA":[]}`), nil
				case "null":
					return archiveResponse(200, `{"code":"0","data":[null]}`), nil
				case "oversized":
					rows := make([]map[string]string, 101)
					for i := range rows {
						rows[i] = row
					}
					return marketResponse(rows), nil
				case "duplicate":
					row["tradeId"] = "11"
					row["px"] = "200"
				case "time_order":
					row["ts"] = strconv.FormatInt(w.From.Add(time.Millisecond).UnixMilli(), 10)
				case "newer_than_seek":
					row["ts"] = strconv.FormatInt(w.To.Add(time.Millisecond).UnixMilli(), 10)
					row["tradeId"] = "12"
				}
				return marketResponse([]map[string]string{wirePublicTrade("11", w.From, "100"), row}), nil
			})
			a, err := c.CollectBoundaryTrades(context.Background(), w)
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.WindowTraversed || len(a.Trades) != 0 || a.Coverage.Pages != 1 || a.Report.Prices != nil {
				t.Fatalf("bad page accepted %+v %v", a, err)
			}
		})
	}
}

func TestBoundaryArchiveRejectsCursorAndConflicts(t *testing.T) {
	for _, kind := range []string{"repeat", "newer_id", "newer_time", "price", "size", "side", "source", "timestamp"} {
		t.Run(kind, func(t *testing.T) {
			w := boundaryWindow()
			pages := 0
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				pages++
				row := wirePublicTrade("10", w.From.Add(time.Millisecond), "100")
				if pages == 2 {
					switch kind {
					case "newer_id":
						row["tradeId"] = "11"
					case "newer_time":
						row["tradeId"] = "9"
						row["ts"] = strconv.FormatInt(w.From.Add(2*time.Millisecond).UnixMilli(), 10)
					case "price":
						row["px"] = "101"
					case "size":
						row["sz"] = "2"
					case "side":
						row["side"] = "sell"
					case "source":
						row["source"] = "1"
					case "timestamp":
						row["ts"] = strconv.FormatInt(w.From.UnixMilli(), 10)
					}
					if kind == "price" || kind == "size" || kind == "side" || kind == "source" || kind == "timestamp" {
						// A conflict must reject the entire otherwise-terminal page,
						// not merely trip the repeated-page progress guard.
						return marketResponse([]map[string]string{row, wirePublicTrade("1", w.From.Add(-time.Millisecond), "90")}), nil
					}
				}
				return marketResponse([]map[string]string{row}), nil
			})
			a, err := c.CollectBoundaryTrades(context.Background(), w)
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.WindowTraversed || len(a.Trades) != 1 || pages != 2 || a.Report.Coverage != "incomplete" {
				t.Fatal("cursor/conflict accepted", a, err)
			}
			if kind == "price" || kind == "size" || kind == "side" || kind == "source" || kind == "timestamp" {
				if a.Coverage.Reason != "market_duplicate_conflict" {
					t.Fatal("conflict hidden by unrelated failure", a.Coverage)
				}
			}
		})
	}
}

func TestBoundaryArchiveScope(t *testing.T) {
	for _, kind := range []string{"future", "old", "minute", "crossing", "zero", "sub_ms", "symbol", "zero_pages", "many_pages", "cancel", "nil_client"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			c, _ := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				return marketResponse([]map[string]string{}), nil
			})
			w := boundaryWindow()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "future":
				w.From = w.From.Add(24 * time.Hour)
				w.To = w.To.Add(24 * time.Hour)
			case "old":
				w.From = w.From.Add(-81 * 24 * time.Hour)
				w.To = w.To.Add(-81 * 24 * time.Hour)
			case "minute":
				w.From = w.From.Truncate(time.Minute)
			case "crossing":
				w.To = w.To.Add(time.Millisecond)
			case "zero":
				w.To = w.From
			case "sub_ms":
				w.From = w.From.Add(time.Nanosecond)
			case "symbol":
				w.Symbol = "SECRET-invalid"
			case "zero_pages":
				w.MaxPages = 0
			case "many_pages":
				w.MaxPages = 201
			case "cancel":
				cancel()
			case "nil_client":
				c = nil
			}
			a, err := c.CollectBoundaryTrades(ctx, w)
			if err == nil || calls != 0 || a.Coverage.Status != "pending" || strings.Contains(err.Error(), "SECRET") {
				t.Fatal("invalid scope reached network", a, err)
			}
		})
	}
}

func TestBoundaryArchiveInterruption(t *testing.T) {
	for _, kind := range []string{"cancel_empty", "cancel_rows", "budget", "quota"} {
		t.Run(kind, func(t *testing.T) {
			w := boundaryWindow()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			c, dir := archiveFixture(t, func(*http.Request) (*http.Response, error) {
				calls++
				rows := []map[string]string{wirePublicTrade(strconv.Itoa(1000-calls), w.From, "100")}
				if strings.HasPrefix(kind, "cancel") {
					cancel()
					if kind == "cancel_empty" {
						rows = []map[string]string{}
					}
				}
				return marketResponse(rows), nil
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
			a, err := c.CollectBoundaryTrades(ctx, w)
			want := 1
			if kind == "budget" {
				want = 200
			}
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.WindowTraversed || calls != want || a.Report.Coverage != "incomplete" {
				t.Fatal("interrupted traversal certified", a.Coverage, err)
			}
			if strings.HasPrefix(kind, "cancel") && len(a.Trades) != 0 {
				t.Fatal("accepted rows after cancellation")
			}
		})
	}
}
