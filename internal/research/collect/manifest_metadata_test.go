//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func studyProtocol() Protocol {
	start := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	return Protocol{Format: "prospective-exit-protocol-v1", ID: "study", Start: start, End: start.Add(28 * 24 * time.Hour), HorizonHours: 72, MaxPages: 200, MaxBytes: 1 << 30}
}
func TestProtocolRegistration(t *testing.T) {
	s := openTestStore(t, storeDir(t), 1<<20)
	p := studyProtocol()
	if _, err := RegisterProtocol(s, p, p.Start.Add(time.Second)); err == nil {
		t.Fatal("backdated initial registration")
	}
	ref, err := RegisterProtocol(s, p, p.Start.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	again, err := RegisterProtocol(s, p, p.Start.Add(time.Hour))
	if err != nil || again != ref {
		t.Fatal("cannot resume", err)
	}
	loaded, err := LoadProtocol(s, ref)
	if err != nil || loaded != p {
		t.Fatal("protocol changed", err)
	}
	for _, kind := range []string{"id", "end", "horizon", "pages", "bytes"} {
		bad := p
		switch kind {
		case "id":
			bad.ID = "other"
		case "end":
			bad.End = bad.End.Add(time.Hour)
		case "horizon":
			bad.HorizonHours = 1
		case "pages":
			bad.MaxPages = 201
		case "bytes":
			bad.MaxBytes = 2 << 30
		}
		if _, err := RegisterProtocol(s, bad, p.Start.Add(-time.Second)); err == nil {
			t.Fatal("changed manifest", kind)
		}
	}
	refs, err := s.Refs()
	if err != nil || len(refs) != 1 || refs[0] != ref {
		t.Fatal("wrong immutable records", err)
	}
	if _, err := LoadProtocol(s, strings.Repeat("0", 64)); err == nil {
		t.Fatal("missing reference accepted")
	}
}
func metadataWire() map[string]string {
	return map[string]string{"instId": "ETH-USDT-SWAP", "instType": "SWAP", "ctType": "linear", "settleCcy": "USDT", "ctValCcy": "ETH", "ctVal": "0.1", "ctMult": "1", "tickSz": "0.01", "lotSz": "0.1", "minSz": "0.1", "state": "live", "extra": "PRIVATE"}
}
func TestMetadataObservation(t *testing.T) {
	for _, kind := range []string{"valid", "empty", "currency", "type", "alias", "decimal", "missing", "wrong_symbol"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := archiveFixture(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/v5/public/instruments" || r.URL.Query().Get("instType") != "SWAP" || len(r.URL.Query()) != 2 || r.Header.Get("OK-ACCESS-KEY") != "" {
					t.Fatal("unsafe metadata request")
				}
				row := metadataWire()
				switch kind {
				case "empty":
					return marketResponse([]map[string]string{}), nil
				case "currency":
					row["settleCcy"] = "BTC"
				case "type":
					row["ctType"] = "inverse"
				case "alias":
					row["TICKSZ"] = "1"
				case "decimal":
					row["tickSz"] = "0"
				case "missing":
					delete(row, "ctMult")
				case "wrong_symbol":
					row["instId"] = "BTC-USDT-SWAP"
				}
				return marketResponse([]map[string]string{row}), nil
			})
			m, err := c.CollectMetadata(context.Background(), "ETH-USDT-SWAP")
			if kind != "valid" {
				if err == nil || m.Status == "complete" {
					t.Fatal("invalid metadata certified")
				}
				return
			}
			if err != nil || m.Status != "complete" || m.HistoryVerified || m.TickSize != "0.01" || m.ObservedAt.IsZero() {
				t.Fatal("metadata lost", m, err)
			}
			b, _ := json.Marshal(m)
			if strings.Contains(string(b), "PRIVATE") {
				t.Fatal("extra field exported")
			}
		})
	}
}
