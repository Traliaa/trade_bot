package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestWarmupAcceptsOnlyClosedValidCandles(t *testing.T) {
	t.Parallel()
	now := time.Now().Truncate(time.Minute)
	client := http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		fmt.Fprintf(w, `{"code":"0","data":[["%d","1","2","1","2","1","1","2","0"],["%d","NaN","2","1","2","1","1","2","1"],["%d","1","2","1","2","1","1","2","1"]]}`, now.UnixMilli(), now.Add(-time.Minute).UnixMilli(), now.Add(-2*time.Minute).UnixMilli())
		return w.Result(), nil
	})}
	s := &Service{client: client}
	cs, err := s.GetCandles(context.Background(), "TEST-USDT-SWAP", "1m", 3)
	if err != nil || len(cs) != 1 || !cs[0].Start.Equal(now.Add(-2*time.Minute)) {
		t.Fatalf("%+v %v", cs, err)
	}
}

func TestGetCandlesReturnsRequestedClosedHistory(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   int
		invalid bool
	}{
		{"LTF_with_live_candle", 50, false},
		{"production_HTF_229_of_230_regression", 230, false},
		{"exchange_page_limit", 300, false},
		{"invalid_row_requires_older_page", 50, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Now().Truncate(time.Hour)
			rows := make([][]string, 0, tc.limit+5)
			for i := 0; i < tc.limit+5; i++ {
				confirm := "1"
				if i == 0 {
					confirm = "0"
				}
				open := "1"
				if tc.invalid && i == 10 {
					open = "NaN"
				}
				rows = append(rows, []string{strconv.FormatInt(now.Add(-time.Duration(i)*time.Hour).UnixMilli(), 10), open, "2", "1", "2", "1", "1", "2", confirm})
			}
			calls := 0
			s := &Service{client: http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
				if limit < 1 || limit > 300 {
					t.Fatalf("invalid OKX page limit: %d", limit)
				}
				if calls == 1 && limit != min(tc.limit+1, 300) {
					t.Fatalf("missing spare candle: %d", limit)
				}
				if r.URL.Query().Get("bar") != "1H" {
					t.Fatalf("wrong OKX timeframe: %s", r.URL.RawQuery)
				}
				after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
				data := make([][]string, 0, limit)
				for _, row := range rows {
					ts, _ := strconv.ParseInt(row[0], 10, 64)
					if after > 0 && ts >= after {
						continue
					}
					data = append(data, row)
					if len(data) == limit {
						break
					}
				}
				w := httptest.NewRecorder()
				if err := json.NewEncoder(w).Encode(map[string]any{"code": "0", "data": data}); err != nil {
					t.Fatal(err)
				}
				return w.Result(), nil
			})}}
			candles, err := s.GetCandles(context.Background(), "RAVE-USDT-SWAP", "1h", tc.limit)
			if err != nil || len(candles) != tc.limit {
				t.Fatalf("got %d/%d closed candles: %v", len(candles), tc.limit, err)
			}
			for i, c := range candles {
				if c.Start.Equal(now) || c.End.After(now) {
					t.Fatal("included live candle")
				}
				if i > 0 && !c.Start.After(candles[i-1].Start) {
					t.Fatal("history not strictly chronological")
				}
			}
			if !candles[len(candles)-1].Start.Equal(now.Add(-time.Hour)) {
				t.Fatal("did not retain latest closed candle")
			}
			wantCalls := 1
			if tc.invalid || tc.limit == 300 {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("requests=%d want=%d", calls, wantCalls)
			}
		})
	}
}

func TestGetCandlesCanceledWithoutRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Service{client: http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("request after shutdown")
		return nil, nil
	})}}
	_, err := s.GetCandles(ctx, "RAVE-USDT-SWAP", "1h", 230)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
