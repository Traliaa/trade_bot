package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHasTpSlQueriesBothSupportedOrderTypes(t *testing.T) {
	c := &Client{apiKey: "test", apiSecret: "test", http: &http.Client{Transport: closeTransport(func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		body := `{"code":"0","data":[]}`
		if q.Get("instId") != "AVAX-USDT-SWAP" {
			t.Errorf("query must be instrument scoped")
		}
		switch q.Get("ordType") {
		case "conditional":
			body = `{"code":"0","data":[{"instId":"AVAX-USDT-SWAP","posSide":"long","state":"live","slTriggerPx":"7.576","tpTriggerPx":""}]}`
		case "oco":
			body = `{"code":"0","data":[{"instId":"AVAX-USDT-SWAP","posSide":"long","state":"live","slTriggerPx":"7","tpTriggerPx":"12"}]}`
		default:
			body = `{"code":"51000","msg":"Parameter ordType error","data":[]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	tp, sl, err := c.HasTpSl(context.Background(), "AVAX-USDT-SWAP", "long")
	if err != nil || !tp || !sl {
		t.Fatalf("tp=%v sl=%v err=%v", tp, sl, err)
	}
}

func TestHasTpSlDoesNotTreatCanceledOrOtherSideAsProtection(t *testing.T) {
	c := &Client{http: &http.Client{Transport: closeTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":"0","data":[{"instId":"AVAX-USDT-SWAP","posSide":"long","state":"canceled","tpTriggerPx":"9"},{"instId":"AVAX-USDT-SWAP","posSide":"short","state":"live","slTriggerPx":"12"},{"instId":"AVAX-USDT-SWAP","posSide":"long","state":"live","slTriggerPx":"NaN"}]}`))}, nil
	})}}
	tp, sl, err := c.HasTpSl(context.Background(), "AVAX-USDT-SWAP", "long")
	if err != nil || tp || sl {
		t.Fatalf("invalid protection accepted: %v %v %v", tp, sl, err)
	}
}
