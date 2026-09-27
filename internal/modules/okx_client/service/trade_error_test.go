package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// A text search for 51155 would wrongly blacklist timeouts or unrelated errors.
func TestPlaceMarketPreservesStructuredRestriction(t *testing.T) {
	for _, tc := range []struct {
		code, message string
		blocked       bool
	}{
		{"51155", "local compliance restrictions", true},
		{"51121", "quantity must be a multiple", false},
		{"51186", "leverage limit", false},
		{"51008", "insufficient balance; reference 51155", false},
	} {
		t.Run(tc.code, func(t *testing.T) {
			c := &Client{apiKey: "test", apiSecret: "test", passph: "test", http: &http.Client{Transport: closeTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/v5/trade/order" {
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				body := fmt.Sprintf(`{"code":"1","msg":"All operations failed","data":[{"sCode":%q,"sMsg":%q}]}`, tc.code, tc.message)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			_, err := c.PlaceMarket(context.Background(), "CL-USDT-SWAP", 1, 1, 0, 1)
			if err == nil {
				t.Fatal("rejection accepted")
			}
			var typed interface {
				error
				EntryRestrictionCode() string
			}
			if !errors.As(fmt.Errorf("PlaceMarket: %w", err), &typed) {
				t.Fatalf("lost structured rejection: %v", err)
			}
			if got := typed.EntryRestrictionCode() != ""; got != tc.blocked {
				t.Fatalf("restriction=%v want %v", got, tc.blocked)
			}
		})
	}
}
