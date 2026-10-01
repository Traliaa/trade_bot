package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"trade_bot/internal/models"
	okx "trade_bot/internal/modules/okx_client/service"
)

// Uses the real OKX client with every HTTP request intercepted. No live orders.
func TestRunnerReconcilesExchangeBeforeRemovingProtection(t *testing.T) {
	for _, mode := range []string{"success", "placement failure", "verification failure", "cancel failure", "closed", "positions failure", "unknown orders", "better stop", "hedge reduceOnly false", "persist failure"} {
		t.Run(mode, func(t *testing.T) {
			previous := http.DefaultTransport
			defer func() { http.DefaultTransport = previous }()
			placed := false
			cancels := []string{}
			http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"code":"0","data":[]}`
				switch r.URL.Path {
				case "/api/v5/account/positions":
					body = `{"code":"0","data":[{"instId":"AVAX-USDT-SWAP","posSide":"long","pos":"1","avgPx":"105","last":"140"}]}`
					if mode == "closed" {
						body = `{"code":"0","data":[]}`
					}
					if mode == "positions failure" {
						return nil, fmt.Errorf("timeout")
					}
				case "/api/v5/public/instruments":
					body = `{"code":"0","data":[{"instId":"AVAX-USDT-SWAP","ctType":"linear","lotSz":"0.1","minSz":"0.1","tickSz":"0.1","ctVal":"1"}]}`
				case "/api/v5/market/ticker":
					body = `{"code":"0","data":[{"last":"140"}]}`
				case "/api/v5/trade/orders-algo-pending":
					if mode == "unknown orders" {
						return nil, fmt.Errorf("timeout")
					}
					if r.URL.Query().Get("ordType") == "conditional" {
						sl := "101"
						if mode == "better stop" {
							sl = "135"
						}
						body = fmt.Sprintf(`{"code":"0","data":[{"algoId":"actual-sl","instId":"AVAX-USDT-SWAP","posSide":"long","side":"sell","state":"live","sz":"1.5","slTriggerPx":"%s","slTriggerPxType":"last","slOrdPx":"-1","reduceOnly":"true"},{"algoId":"actual-tp","instId":"AVAX-USDT-SWAP","posSide":"long","side":"sell","state":"live","sz":"1","tpTriggerPx":"150","tpOrdPx":"-1","reduceOnly":"true"}%s]}`, sl, func() string {
							if placed && mode != "verification failure" {
								p := "130"
								if mode == "better stop" {
									p = "135"
								}
								return fmt.Sprintf(`,{"algoId":"new-sl","instId":"AVAX-USDT-SWAP","posSide":"long","side":"sell","state":"live","sz":"1","slTriggerPx":"%s","slTriggerPxType":"last","slOrdPx":"-1","reduceOnly":"true"}`, p)
							}
							return ""
						}())
					}
				case "/api/v5/trade/order-algo":
					var order map[string]string
					if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
						t.Fatal(err)
					}
					want := "130"
					if mode == "better stop" {
						want = "135"
					}
					if order["slTriggerPx"] != want || order["sz"] != "1" || order["reduceOnly"] != "true" || order["tpTriggerPx"] != "" {
						t.Fatalf("unsafe order: %+v", order)
					}
					if mode == "placement failure" {
						body = `{"code":"1","data":[{"sCode":"51000","sMsg":"rejected"}]}`
					} else {
						placed = true
						body = `{"code":"0","data":[{"algoId":"new-sl","sCode":"0"}]}`
					}
				case "/api/v5/trade/cancel-algos":
					var orders []map[string]string
					_ = json.NewDecoder(r.Body).Decode(&orders)
					if !placed || mode == "verification failure" {
						t.Fatal("canceled protection without confirmed replacement")
					}
					id := orders[0]["algoId"]
					cancels = append(cancels, id)
					if id != "actual-sl" && id != "actual-tp" {
						t.Fatalf("used stale database algo id: %s", id)
					}
					body = `{"code":"0","data":[{"sCode":"0"}]}`
					if mode == "cancel failure" {
						body = `{"code":"1","msg":"rejected"}`
					}
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				if mode == "hedge reduceOnly false" {
					body = strings.ReplaceAll(body, `"reduceOnly":"true"`, `"reduceOnly":"false"`)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			c := okx.NewClient(&models.UserSettings{})
			st := models.PositionTrailState{InstID: "AVAX-USDT-SWAP", PosSide: "long", Entry: 100, SL: 101, RiskDist: 10, Size: 2, AlgoID: "stale-db-sl", TPAlgoID: "stale-db-tp", TookPartial: true}
			_, err := reconcileRunnerProtection(context.Background(), c, &st, func() error {
				if mode == "persist failure" {
					return fmt.Errorf("DB unavailable")
				}
				return nil
			})
			success := mode == "success" || mode == "better stop" || mode == "hedge reduceOnly false"
			if (err == nil) != success {
				t.Fatalf("unexpected result mode=%s err=%v", mode, err)
			}
			if success && (len(cancels) != 2 || st.Size != 1 || st.AlgoID != "new-sl" || st.TPAlgoID != "" || !st.ProfitRunnerActive) {
				t.Fatalf("not reconciled: state=%+v cancels=%v", st, cancels)
			}
			if st.Entry != 100 || st.RiskDist != 10 {
				t.Fatal("initial risk or entry changed")
			}
			if !placed && len(cancels) != 0 {
				t.Fatal("lost old protection")
			}
		})
	}
}

func TestShortRunnerReplacesOCOOnlyAfterStandaloneSLConfirmed(t *testing.T) {
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	placed, verified, canceled := false, false, false
	http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"code":"0","data":[]}`
		switch r.URL.Path {
		case "/api/v5/account/positions":
			body = `{"code":"0","data":[{"instId":"TEST-USDT-SWAP","posSide":"short","pos":"1","avgPx":"100","last":"60"}]}`
		case "/api/v5/public/instruments":
			body = `{"code":"0","data":[{"instId":"TEST-USDT-SWAP","ctType":"linear","lotSz":"1","minSz":"1","tickSz":"0.1","ctVal":"1"}]}`
		case "/api/v5/market/ticker":
			body = `{"code":"0","data":[{"last":"60"}]}`
		case "/api/v5/trade/orders-algo-pending":
			if r.URL.Query().Get("ordType") == "oco" && !canceled {
				body = `{"code":"0","data":[{"algoId":"old-oco","instId":"TEST-USDT-SWAP","posSide":"short","side":"buy","state":"live","sz":"1","slTriggerPx":"105","slTriggerPxType":"last","slOrdPx":"-1","tpTriggerPx":"50","reduceOnly":"false"}]}`
			}
			if r.URL.Query().Get("ordType") == "conditional" && placed {
				verified = true
				body = `{"code":"0","data":[{"algoId":"new-sl","instId":"TEST-USDT-SWAP","posSide":"short","side":"buy","state":"live","sz":"1","slTriggerPx":"70","slTriggerPxType":"last","slOrdPx":"-1","reduceOnly":"false"}]}`
			}
		case "/api/v5/trade/order-algo":
			var order map[string]string
			_ = json.NewDecoder(r.Body).Decode(&order)
			if canceled || order["slTriggerPx"] != "70" || order["side"] != "buy" || order["posSide"] != "short" || order["sz"] != "1" {
				t.Fatalf("unsafe short stop %+v", order)
			}
			placed = true
			body = `{"code":"0","data":[{"algoId":"new-sl","sCode":"0"}]}`
		case "/api/v5/trade/cancel-algos":
			var ids []map[string]string
			_ = json.NewDecoder(r.Body).Decode(&ids)
			if !verified || canceled || ids[0]["algoId"] != "old-oco" {
				t.Fatal("OCO canceled before confirmed replacement or twice")
			}
			canceled = true
			body = `{"code":"0","data":[{"algoId":"old-oco","sCode":"0"}]}`
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	st := models.PositionTrailState{InstID: "TEST-USDT-SWAP", PosSide: "short", Entry: 100, RiskDist: 10, SL: 110, Size: 1, TookPartial: true}
	_, err := reconcileRunnerProtection(context.Background(), okx.NewClient(&models.UserSettings{}), &st, func() error { return nil })
	if err != nil || !canceled || st.SL != 70 || st.TPAlgoID != "" {
		t.Fatalf("short OCO not converted: %+v err=%v", st, err)
	}
}
