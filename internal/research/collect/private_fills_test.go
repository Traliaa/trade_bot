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

func fillWindow() FillWindow {
	return FillWindow{Symbol: "ETH-USDT-SWAP", From: time.Date(2026, 10, 8, 5, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 8, 6, 0, 0, 0, time.UTC), MaxPages: 200}
}
func wireFill(bill string, at time.Time) map[string]string {
	return map[string]string{"billId": bill, "tradeId": "771", "ordId": "551", "instType": "SWAP", "instId": "ETH-USDT-SWAP", "side": "buy", "posSide": "long", "fillSz": "0.4", "fillPx": "100.00", "fillPnl": "0", "fee": "-0.01", "feeCcy": "USDT", "ts": strconv.FormatInt(at.UnixMilli(), 10), "fillTime": strconv.FormatInt(at.Add(-time.Second).UnixMilli(), 10), "clOrdId": "PRIVATE-unused", "tag": "PRIVATE-discard"}
}
func fillResponse(rows ...map[string]string) *http.Response {
	if rows == nil {
		rows = []map[string]string{}
	}
	b, _ := json.Marshal(map[string]any{"code": "0", "msg": "", "data": rows})
	return archiveResponse(200, string(b))
}
func TestPrivateFillsOverlapTiesAndCoverage(t *testing.T) {
	w := fillWindow()
	calls := 0
	config := 0
	p, dir := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == accountConfigPath {
			config++
			return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456"}]}`), nil
		}
		calls++
		q := r.URL.Query()
		if q.Get("begin") != strconv.FormatInt(w.From.Add(-time.Millisecond).UnixMilli(), 10) || q.Get("end") != strconv.FormatInt(w.To.UnixMilli(), 10) || q.Get("limit") != "100" {
			t.Fatal("wrong record window query")
		}
		switch calls {
		case 1:
			if q.Get("after") != "" {
				t.Fatal("private cursor reused across passes")
			}
			rows := []map[string]string{}
			for i := 300; i >= 201; i-- {
				rows = append(rows, wireFill(strconv.Itoa(i), w.From))
			}
			return fillResponse(rows...), nil
		case 2:
			if q.Get("after") != "201" {
				t.Fatal("timestamp used as cursor: ties would be skipped")
			}
			return fillResponse(wireFill("201", w.From), wireFill("200", w.From)), nil
		case 3:
			if q.Get("after") != "200" {
				t.Fatal("cursor did not progress")
			}
			return fillResponse(), nil
		default:
			return fillResponse(), nil
		}
	})
	a, err := p.CollectFills(context.Background(), w)
	if err != nil || !a.Coverage.RecordWindowComplete || a.Coverage.Status != "complete" || a.Coverage.Pages != 3 || a.Coverage.Fills != 101 || len(a.fills) != 101 || a.Coverage.ExecutionOutsideWindow != 101 {
		t.Fatalf("unexpected coverage %+v %v", a.Coverage, err)
	}
	if !a.fills[0].Fill.At.Equal(w.From.Add(-time.Second)) || !a.fills[0].RecordedAt.Equal(w.From) {
		t.Fatal("ts confused with fillTime")
	}
	for _, v := range []any{a, a.fills[0]} {
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("private evidence serializable")
		}
		if out := fmt.Sprintf("%#v", v); strings.Contains(out, "551") || strings.Contains(out, "771") {
			t.Fatal("private evidence formatted")
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "PRIVATE") || strings.Contains(string(b), "test-secret") || strings.Contains(string(b), "billId") || strings.Contains(string(b), "ordId") {
			t.Fatal("private data persisted")
		}
	}
	_, err = p.CollectFills(context.Background(), w)
	if err != nil || config != 2 {
		t.Fatal("account check cached across passes")
	}
}
func TestPrivateFillsBoundariesAndShortPages(t *testing.T) {
	w := fillWindow()
	pages := 0
	p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == accountConfigPath {
			return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456"}]}`), nil
		}
		pages++
		switch pages {
		case 1:
			return fillResponse(wireFill("30", w.To), wireFill("20", w.From), wireFill("10", w.From.Add(-time.Millisecond))), nil
		default:
			return fillResponse(), nil
		}
	})
	a, err := p.CollectFills(context.Background(), w)
	if err != nil || a.Coverage.Fills != 1 || pages != 2 || !a.Coverage.RecordWindowComplete || a.fills[0].Fill.ID != "20" {
		t.Fatal("half-open window or exhaustion wrong", a.Coverage, err)
	}
}
func TestPrivateFillsRejectsMalformedPages(t *testing.T) {
	for _, kind := range []string{"case_alias", "conflict", "repeat", "wrong_symbol", "wrong_type", "no_order", "timestamp", "missing_fill_time", "price", "size", "fee", "missing_fee_currency", "bill", "out_of_bounds", "too_many", "newer_cursor"} {
		t.Run(kind, func(t *testing.T) {
			w := fillWindow()
			pages := 0
			p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == accountConfigPath {
					return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456"}]}`), nil
				}
				pages++
				row := wireFill("100", w.From)
				switch kind {
				case "case_alias":
					row["FILLSZ"] = "999"
				case "conflict":
					if pages == 1 {
						return fillResponse(row), nil
					}
					row["fillSz"] = "0.5"
				case "repeat":
				case "newer_cursor":
					if pages == 1 {
						return fillResponse(row), nil
					}
					row["billId"] = "101"
				case "wrong_symbol":
					row["instId"] = "BTC-USDT-SWAP"
				case "wrong_type":
					row["instType"] = "SPOT"
				case "no_order":
					row["ordId"] = ""
				case "timestamp":
					row["ts"] = "1e12"
				case "missing_fill_time":
					delete(row, "fillTime")
				case "price":
					row["fillPx"] = "NaN"
				case "size":
					row["fillSz"] = "0"
				case "fee":
					row["fee"] = ""
				case "missing_fee_currency":
					row["feeCcy"] = ""
				case "bill":
					row["billId"] = "PRIVATE-malformed"
				case "out_of_bounds":
					row["ts"] = strconv.FormatInt(w.To.Add(time.Millisecond).UnixMilli(), 10)
				case "too_many":
					rows := []map[string]string{}
					for i := 1; i <= 101; i++ {
						rows = append(rows, wireFill(strconv.Itoa(i), w.From))
					}
					return fillResponse(rows...), nil
				}
				return fillResponse(row), nil
			})
			a, err := p.CollectFills(context.Background(), w)
			if err == nil || a.Coverage.Status != "incomplete" || a.Coverage.RecordWindowComplete || pages > 2 || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("bad page accepted: %+v err=%v", a.Coverage, err)
			}
		})
	}
}
func TestPrivateFillsPageBudget(t *testing.T) {
	w := fillWindow()
	pages := 0
	p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == accountConfigPath {
			return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456"}]}`), nil
		}
		pages++
		return fillResponse(wireFill(strconv.Itoa(1000-pages), w.From)), nil
	})
	a, err := p.CollectFills(context.Background(), w)
	if err == nil || pages != 200 || a.Coverage.RecordWindowComplete || a.Coverage.Reason != "page_budget_exhausted" {
		t.Fatalf("page budget bypassed: %+v %v", a.Coverage, err)
	}
}
func TestPrivateFillsScopeAndPermission(t *testing.T) {
	for _, kind := range []string{"old", "future", "submillisecond", "wide", "empty", "zero_pages", "many_pages", "wrong_symbol", "cancel", "permissions"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != accountConfigPath {
					t.Fatal("fills after denied config")
				}
				return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only,trade","uid":"123456"}]}`), nil
			})
			w := fillWindow()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "old":
				w.From = w.From.Add(-90 * 24 * time.Hour)
				w.To = w.From.Add(time.Hour)
			case "future":
				w.To = w.To.Add(24 * time.Hour)
			case "submillisecond":
				w.From = w.From.Add(time.Nanosecond)
			case "wide":
				w.From = w.From.Add(-32 * 24 * time.Hour)
			case "empty":
				w.To = w.From
			case "zero_pages":
				w.MaxPages = 0
			case "many_pages":
				w.MaxPages = 201
			case "wrong_symbol":
				w.Symbol = "ETH-USDC-SWAP"
			case "cancel":
				cancel()
			}
			a, err := p.CollectFills(ctx, w)
			if err == nil || a.Coverage.RecordWindowComplete {
				t.Fatal("bad scope accepted")
			}
			if kind != "permissions" && calls != 0 {
				t.Fatal("invalid scope reached server")
			}
			if kind == "permissions" && calls != 1 {
				t.Fatal("unexpected denied pass calls")
			}
		})
	}
}

func TestPrivateFillsInterruptedPass(t *testing.T) {
	for _, kind := range []string{"cancel", "cancel_empty", "quota"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			w := fillWindow()
			calls := 0
			p, dir := privateFixture(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == accountConfigPath {
					return archiveResponse(200, `{"code":"0","data":[{"perm":"read_only","uid":"123456"}]}`), nil
				}
				calls++
				if kind == "cancel" || kind == "cancel_empty" {
					cancel()
				}
				if kind == "cancel_empty" {
					return fillResponse(), nil
				}
				return fillResponse(wireFill("100", w.From)), nil
			})
			if kind == "quota" {
				var journal strings.Builder
				for i := 1; i <= dailyRequests-2; i++ {
					fmt.Fprintf(&journal, "%d\n", i)
				}
				if err := os.WriteFile(filepath.Join(dir, ".quota-2026-10-08"), []byte(journal.String()), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := p.CollectFills(ctx, w)
			if err == nil || calls != 1 || a.Coverage.Status != "incomplete" || a.Coverage.RecordWindowComplete || strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("interrupted archive marked complete: %+v %v", a.Coverage, err)
			}
		})
	}
}

func TestPrivateFillsRechecksChangedPermissionsAndPreservesCurrency(t *testing.T) {
	w := fillWindow()
	config, pages := 0, 0
	p, _ := privateFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == accountConfigPath {
			config++
			perm := "read_only"
			if config > 1 {
				perm = "read_only,trade"
			}
			return archiveResponse(200, `{"code":"0","data":[{"perm":"`+perm+`","uid":"123456"}]}`), nil
		}
		pages++
		if pages == 1 {
			row := wireFill("100", w.From)
			row["feeCcy"] = "OKB"
			return fillResponse(row), nil
		}
		return fillResponse(), nil
	})
	a, err := p.CollectFills(context.Background(), w)
	if err != nil || len(a.fills) != 1 || a.fills[0].Fill.FeeCurrency != "OKB" {
		t.Fatal("non-USDT fee converted or discarded", err)
	}
	a, err = p.CollectFills(context.Background(), w)
	if err == nil || pages != 2 || config != 2 || a.Coverage.RecordWindowComplete {
		t.Fatal("changed key permissions did not block second pass")
	}
}

func TestPrivateFillsRejectCaseAliases(t *testing.T) {
	w := fillWindow()
	row := wireFill("100", w.From)
	row["FILLSZ"] = "999"
	b, _ := json.Marshal(map[string]any{"code": "0", "data": []map[string]string{row}})
	if _, err := decodePrivateFills(b, w, w.To.Add(time.Hour)); err == nil {
		t.Fatal("ambiguous fill field accepted")
	}
}
