package sessions

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okx "trade_bot/internal/modules/okx_client/service"
	"trade_bot/internal/modules/repository/pg"
	"trade_bot/pkg/db"
)

// Real session and SQL, fake HTTP exchange: never submits a real order.
func TestCaptureOnOffTradeParity(t *testing.T) {
	dsn := os.Getenv("TRADE_REPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated loopback trade_report_test")
	}
	c, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if c.ConnConfig.Host != "127.0.0.1" || c.ConnConfig.Database != "trade_report_test" {
		t.Fatal("local test DB only")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, f := range []string{"0003_add_trade_history.sql", "0007_instrument_entry_blocks.sql", "0008_add_research_entry_snapshot.sql"} {
		raw, err := os.ReadFile("../../../../migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(raw), "-- +goose Down")[0]
		up = strings.ReplaceAll(up, "CREATE TABLE public.instrument_entry_blocks", "CREATE TABLE IF NOT EXISTS public.instrument_entry_blocks")
		if _, err = pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for _, failSL := range []bool{false, true} {
		t.Run(fmt.Sprintf("sl_failure_%t", failSL), func(t *testing.T) {
			var baseline []string
			for _, enabled := range []bool{false, true} {
				cfg := models.Settings{TradingSettings: models.TradingSettings{RiskPct: 10, Leverage: 2, StopPct: 5, TakeProfitRR: 3, OKXAPIKey: "fixture", OKXAPISecret: "fixture", OKXPassphrase: "fixture"}}
				user := &models.UserSettings{TelegramID: time.Now().UnixNano(), Settings: cfg}
				s := &UserSession{Base: base.Base{Logger: zap.NewNop()}, Config: &config.Config{ResearchCapture: config.ResearchCaptureConfig{Enabled: enabled, ProtocolID: "test"}}, User: user, Repo: pg.NewUser(db.NewPgTxManager(pool)), Okx: okx.NewClient(user), Notifier: &protectionNotifier{}}
				s.InitSettings(cfg)
				calls := []string{}
				algos := 0
				http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
					raw := []byte{}
					if r.Body != nil {
						raw, _ = io.ReadAll(r.Body)
					}
					calls = append(calls, r.Method+" "+r.URL.RequestURI()+" "+string(raw))
					body := ""
					switch r.URL.Path {
					case "/api/v5/public/instruments":
						body = `{"code":"0","data":[{"instId":"ETH-USDT-SWAP","ctType":"linear","settleCcy":"USDT","ctValCcy":"ETH","lotSz":"0.1","minSz":"0.1","tickSz":"0.01","ctVal":"1","ctMult":"1"}]}`
					case "/api/v5/market/ticker":
						body = `{"code":"0","data":[{"last":"100"}]}`
					case "/api/v5/account/positions":
						body = `{"code":"0","data":[]}`
					case "/api/v5/account/set-leverage":
						body = `{"code":"0","data":[]}`
					case "/api/v5/trade/order":
						body = `{"code":"0","data":[{"ordId":"fixture-order","sCode":"0"}]}`
					case "/api/v5/trade/fills":
						body = `{"code":"0","data":[{"instId":"ETH-USDT-SWAP","posSide":"long","side":"buy","ordId":"fixture-order","fillPx":"101","fillSz":"2","fee":"-0.01","fillPnl":"0","tradeId":"fixture-fill","ts":"1791356400000"}]}`
					case "/api/v5/trade/order-algo":
						algos++
						if failSL {
							body = `{"code":"1","data":[{"sCode":"51000","sMsg":"fixture rejection"}]}`
						} else {
							body = `{"code":"0","data":[{"algoId":"fixture-algo","sCode":"0"}]}`
						}
					default:
						t.Fatalf("unexpected HTTP request: %s", r.URL.Path)
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
				})
				p, err := s.CalcTradeParams(ctx, "ETH-USDT-SWAP", "BUY", 100)
				if err != nil {
					t.Fatal(err)
				}
				result, err := s.OpenPositionWithTpSl(ctx, models.Signal{InstID: "ETH-USDT-SWAP"}, p)
				if (err != nil) != failSL {
					t.Fatalf("unexpected open outcome: %v", err)
				}
				if !failSL {
					if p.Entry != 101 || p.Size != 2 || algos != 2 {
						t.Fatal("changed fill/protection behavior")
					}
					if enabled && (p.ResearchEntry.Planned.Entry != 100 || p.ResearchEntry.Planned.Size != 2 || result.ResearchEvidence.TimeSource != "exchange_fills" || len(p.ResearchEntry.Settings) != 3) {
						t.Fatal("entry observations not wired")
					}
				} else if algos != 1 || !strings.Contains(calls[len(calls)-1], `"reduceOnly":true`) {
					t.Fatal("emergency close changed")
				}
				if !enabled {
					baseline = calls
				} else if !reflect.DeepEqual(baseline, calls) {
					t.Fatalf("capture altered exchange call sequence: off=%v on=%v", baseline, calls)
				}
			}
		})
	}
}
