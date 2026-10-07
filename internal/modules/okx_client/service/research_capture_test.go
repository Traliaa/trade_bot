package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCaptureMetadataFromUsedResponse(t *testing.T) {
	count := 0
	at := time.Now()
	c := &Client{http: &http.Client{Transport: closeTransport(func(r *http.Request) (*http.Response, error) {
		count++
		body := `{"code":"0","data":[{"last":"100"}]}`
		if r.URL.Path == "/api/v5/public/instruments" {
			body = `{"code":"0","data":[{"instId":"ETH-USDT-SWAP","ctType":"linear","settleCcy":"USDT","ctValCcy":"ETH","lotSz":"0.10","minSz":"0.1","tickSz":"0.01","ctVal":"0.1","ctMult":"10"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	m, err := c.GetInstrumentMeta(context.Background(), "ETH-USDT-SWAP")
	if err != nil {
		t.Fatal(err)
	}
	o := m.ResearchMetadata
	if count != 2 || o == nil || o.RawLotSz != "0.10" || o.RawCtVal != "0.1" || o.RawCtMult != "10" || o.EffectiveCtVal != 1 || m.CtVal != 1 || o.ExchangeAt != nil || o.ReceivedAt.Before(at) {
		t.Fatalf("wrong source observation: %+v calls=%d", o, count)
	}
}

func TestCaptureUsesExecutionTimeWithoutChangingLegacyTime(t *testing.T) {
	for _, fillTime := range []string{"1791356400000", "", "bad", "-1"} {
		c := &Client{apiKey: "test", apiSecret: "test", passph: "test", http: &http.Client{Transport: closeTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"code":"0","data":[{"instId":"ETH-USDT-SWAP","fillPx":"100","fillSz":"1","ts":"1791356400001","fillTime":"` + fillTime + `"}]}`
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		fills, err := c.RecentFills(context.Background(), "ETH-USDT-SWAP", 100)
		if err != nil {
			t.Fatal(err)
		}
		if fills[0].FillTime.UnixMilli() != 1791356400001 {
			t.Fatal("changed trading timestamp")
		}
		if fillTime == "1791356400000" {
			if fills[0].ResearchExecutionTime == nil || fills[0].ResearchExecutionTime.UnixMilli() != 1791356400000 {
				t.Fatal("wrong execution time")
			}
		} else if fills[0].ResearchExecutionTime != nil {
			t.Fatal("invented missing execution time")
		}
	}
}
