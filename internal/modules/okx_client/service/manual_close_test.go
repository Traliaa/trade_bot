package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type closeTransport func(*http.Request) (*http.Response, error)

func (f closeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSubmitManualCloseIsReduceOnlyAndSigned(t *testing.T) {
	for _, side := range []string{"long", "short"} {
		t.Run(side, func(t *testing.T) {
			calls := 0
			c := &Client{apiKey: "test", apiSecret: "test", http: &http.Client{Transport: closeTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				wantSide := "sell"
				if side == "short" {
					wantSide = "buy"
				}
				if body["side"] != wantSide || body["posSide"] != side || body["reduceOnly"] != true || body["clOrdId"] != "request123" || body["sz"] != "4" {
					t.Fatalf("wrong order: %v", body)
				}
				if r.Header.Get("OK-ACCESS-SIGN") == "" {
					t.Fatal("unsigned order")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":"0","data":[{"ordId":"42","sCode":"0"}]}`))}, nil
			})}}
			id, err := c.SubmitManualClose(context.Background(), "ETH-USDT-SWAP", side, 4, "request123")
			if err != nil || id != "42" || calls != 1 {
				t.Fatalf("%s %v", id, err)
			}
		})
	}
}
