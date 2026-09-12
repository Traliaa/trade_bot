package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"trade_bot/internal/modules/config"
)

type fakeServer struct {
	URL    string
	client http.Client
}

func (f *fakeServer) Close()               {}
func (f *fakeServer) Client() *http.Client { return &f.client }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func selectionServer(t *testing.T) *fakeServer {
	t.Helper()
	now := time.Now()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/market/tickers" {
			fmt.Fprintf(w, `{"code":"0","data":[{"instId":"BTC-USDT-SWAP","last":"60000","open24h":"60000","high24h":"61000","low24h":"59000","volCcy24h":"400","bidPx":"59999","askPx":"60001","ts":"%d"}]}`, now.UnixMilli())
			return
		}
		fmt.Fprintf(w, `{"code":"0","data":[{"instId":"BTC-USDT-SWAP","state":"live","listTime":"%d","ctVal":"0.01","ctValCcy":"BTC","minSz":"0.01","lotSz":"0.01","lever":"10"}]}`, now.Add(-100*24*time.Hour).UnixMilli())
	})
	return &fakeServer{URL: "http://fixture.invalid", client: http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Result(), nil
	})}}
}
func TestSelectionTotalLimitAndNoSharedConfigMutation(t *testing.T) {
	server := selectionServer(t)
	defer server.Close()
	cfg := &config.Config{}
	cfg.Strategy.WatchTopN = 20
	s := &Service{endpoint: server.URL + "/market", cfg: cfg, client: *server.Client()}
	symbols, err := s.SelectUniverse(20, "conservative")
	if err != nil || len(symbols) != 1 || symbols[0] != "BTC-USDT-SWAP" {
		t.Fatal(symbols, err)
	}
	if cfg.Strategy.WatchTopN != 20 || len(cfg.Strategy.Symbols) != 0 {
		t.Fatal("mutated config")
	}
}
func TestRotationPreparesBeforePromotionAndPreservesOldStreams(t *testing.T) {
	server := selectionServer(t)
	defer server.Close()
	cfg := &config.Config{}
	cfg.Strategy.WatchTopN = 1
	cfg.Strategy.LTF = "15m"
	cfg.Strategy.HTF = "1h"
	s := &Service{endpoint: server.URL + "/market", cfg: cfg, client: *server.Client(), watch: []string{"OLD"}, selectedSince: map[string]time.Time{"OLD": time.Now()}}
	retained := map[string]bool{"OLD": true, "BTC-USDT-SWAP": true}
	s.prepareRotation = func(context.Context, []string) error { return fmt.Errorf("warmup failed") }
	if s.rotate(context.Background(), retained, nil) == nil || !s.Selected("OLD") {
		t.Fatal("promoted failed warmup")
	}
	s.prepareRotation = func(_ context.Context, syms []string) error {
		if !s.Selected("OLD") || len(syms) != 1 {
			t.Fatal("premature selection")
		}
		return nil
	}
	if err := s.rotate(context.Background(), retained, nil); err != nil {
		t.Fatal(err)
	}
	if s.Selected("OLD") || !retained["OLD"] || s.EntryAllowed("BTC-USDT-SWAP") {
		t.Fatal("unsafe admission or dropped trailing")
	}
	for _, tf := range []string{"1m", "15m", "1H"} {
		s.markMarketData(tf, time.Now(), "BTC-USDT-SWAP")
	}
	if !s.EntryAllowed("BTC-USDT-SWAP") {
		t.Fatal("fresh selected entry blocked")
	}
}
