package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	okx "trade_bot/internal/modules/okx_client/service"
	"trade_bot/internal/modules/repository/pg"
	"trade_bot/pkg/db"
)

func TestProfitRunnerSessionPostgres(t *testing.T) {
	dsn := os.Getenv("TRADE_REPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated local trade_report_test")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "trade_report_test" {
		t.Fatal("local test DB only")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, file := range []string{"0003_add_trade_history.sql", "0004_add_trade_fills.sql", "0005_manual_close_requests.sql"} {
		raw, err := os.ReadFile("../../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(raw), "-- +goose Down")[0]
		up = strings.ReplaceAll(up, "CREATE TABLE public.manual_close_requests", "CREATE TABLE IF NOT EXISTS public.manual_close_requests")
		up = strings.ReplaceAll(up, "CREATE UNIQUE INDEX manual_close_one_active", "CREATE UNIQUE INDEX IF NOT EXISTS manual_close_one_active")
		if _, err := pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	for _, mode := range []string{"partial", "existing partial", "ambiguous partial", "stop failure", "missing saved SL ID", "manual pending"} {
		t.Run(mode, func(t *testing.T) {
			repo := pg.NewUser(db.NewPgTxManager(pool))
			user := &models.UserSettings{TelegramID: 921002, Settings: models.Settings{TrailingConfig: models.TrailingConfig{PartialEnabled: true, PartialTriggerR: 1.25, PartialCloseFrac: .5}}}
			user.Settings.TradingSettings.OKXAPIKey = "test"
			user.Settings.TradingSettings.OKXAPISecret = "test"
			user.Settings.TradingSettings.OKXPassphrase = "test"
			tr := models.TradeRecord{GUID: uuid.New(), UserID: user.TelegramID, InstID: "RUNNER-TEST-USDT-SWAP", Status: models.TradeStatusOpen, EntryAt: time.Now().Add(-time.Hour), Payload: models.TradePayload{PosSide: "long", EntryPrice: 100, EntrySize: 2, CurrentSize: 2, StopLoss: 90, RiskDist: 10, AlgoID: "db-old-id", TPAlgoID: "db-old-tp", TakeProfit: 120}}
			if mode == "existing partial" {
				tr.Payload.CurrentSize = 1
				tr.Payload.PartialCount = 1
			}
			if mode == "missing saved SL ID" {
				tr.Payload.AlgoID = ""
				tr.Payload.TookPartial = true
			}
			tr.CreatedAt = time.Now()
			tr.UpdatedAt = time.Now()
			if err := repo.CreateTradeHistory(ctx, tr); err != nil {
				t.Fatal(err)
			}
			defer func() {
				_, _ = pool.Exec(ctx, "DELETE FROM public.manual_close_requests WHERE trade_guid=$1", tr.GUID)
				_, _ = pool.Exec(ctx, "DELETE FROM public.trade_history WHERE guid=$1", tr.GUID)
			}()
			if mode == "manual pending" {
				if _, err := pool.Exec(ctx, "INSERT INTO public.manual_close_requests(request_id,trade_guid,fraction,status) VALUES($1,$2,.5,'accepted')", uuid.New(), tr.GUID); err != nil {
					t.Fatal(err)
				}
			}
			st := trailStateFromTrade(tr)
			key := helper.TrailKey(tr.InstID, "long")
			s := &UserSession{Base: base.Base{Logger: zap.NewNop()}, User: user, Repo: repo, Okx: okx.NewClient(user), Notifier: &protectionNotifier{}, LastMsgAt: map[string]time.Time{}, TrailStates: map[models.PosKey]*models.PositionTrailState{key: st}}
			s.InitSettings(user.Settings)
			liveSize := tr.Payload.CurrentSize
			price := 140.0
			partialCalls := 0
			stopCalls := 0
			orders := map[string]map[string]string{"live-old": {"algoId": "live-old", "slTriggerPx": "101", "sz": "2"}, "live-tp": {"algoId": "live-tp", "tpTriggerPx": "150", "sz": "2"}}
			previous := http.DefaultTransport
			defer func() { http.DefaultTransport = previous }()
			http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
				body := `{"code":"0","data":[]}`
				switch r.URL.Path {
				case "/api/v5/account/positions":
					body = fmt.Sprintf(`{"code":"0","data":[{"instId":"RUNNER-TEST-USDT-SWAP","posSide":"long","pos":"%g","avgPx":"105","last":"%g"}]}`, liveSize, price)
				case "/api/v5/public/instruments":
					body = `{"code":"0","data":[{"instId":"RUNNER-TEST-USDT-SWAP","ctType":"linear","lotSz":"0.1","minSz":"0.1","tickSz":"0.1","ctVal":"1"}]}`
				case "/api/v5/market/ticker":
					body = fmt.Sprintf(`{"code":"0","data":[{"last":"%g"}]}`, price)
				case "/api/v5/trade/orders-algo-pending":
					if r.URL.Query().Get("ordType") == "conditional" {
						list := []map[string]string{}
						for _, order := range orders {
							row := map[string]string{"instId": tr.InstID, "posSide": "long", "side": "sell", "state": "live", "slTriggerPxType": "last", "slOrdPx": "-1", "tpOrdPx": "-1", "reduceOnly": "true"}
							for k, v := range order {
								row[k] = v
							}
							list = append(list, row)
						}
						raw, _ := json.Marshal(map[string]any{"code": "0", "data": list})
						body = string(raw)
					}
				case "/api/v5/trade/order-algo":
					stopCalls++
					var order map[string]string
					_ = json.NewDecoder(r.Body).Decode(&order)
					if order["tpTriggerPx"] != "" {
						t.Fatal("recreated passed TP")
					}
					if mode == "stop failure" {
						body = `{"code":"1","data":[{"sCode":"51000"}]}`
						break
					}
					id := fmt.Sprintf("sl-%d", stopCalls)
					order["algoId"] = id
					orders[id] = order
					body = fmt.Sprintf(`{"code":"0","data":[{"algoId":"%s","sCode":"0"}]}`, id)
				case "/api/v5/trade/cancel-algos":
					saved, err := repo.GetByGUID(ctx, tr.GUID)
					if err != nil {
						t.Fatal(err)
					}
					if !saved.Payload.ProfitRunnerActive || saved.Payload.CurrentStopLoss != price-10 {
						t.Fatal("cancel before persisting confirmed runner SL")
					}
					var ids []map[string]string
					_ = json.NewDecoder(r.Body).Decode(&ids)
					if stopCalls == 0 || mode == "stop failure" {
						t.Fatal("cancel before replacement")
					}
					delete(orders, ids[0]["algoId"])
					if mode == "manual pending" {
						// Manual order fills AFTER runner's first position read.
						liveSize = 1
						if _, err := pool.Exec(ctx, "UPDATE public.manual_close_requests SET status='filled' WHERE trade_guid=$1", tr.GUID); err != nil {
							t.Fatal(err)
						}
					}
					body = `{"code":"0","data":[{"sCode":"0"}]}`
				case "/api/v5/trade/order":
					partialCalls++
					saved, err := repo.GetByGUID(ctx, tr.GUID)
					if err != nil {
						t.Fatal(err)
					}
					if !saved.Payload.RunnerPartialPending {
						t.Fatal("partial sent before persistent intent")
					}
					if mode == "ambiguous partial" {
						return nil, fmt.Errorf("connection lost after send")
					}
					var order map[string]any
					_ = json.NewDecoder(r.Body).Decode(&order)
					if order["sz"] != "1" || order["reduceOnly"] != true {
						t.Fatalf("wrong partial %+v", order)
					}
					liveSize = 1
					body = `{"code":"0","data":[{"ordId":"partial-1","sCode":"0"}]}`
				case "/api/v5/trade/fills":
					body = `{"code":"0","data":[{"instId":"RUNNER-TEST-USDT-SWAP","tradeId":"runner-fill","ordId":"partial-1","side":"sell","posSide":"long","fillPx":"140","fillSz":"1","ts":"1790856000000"}]}`
				default:
					t.Fatalf("unexpected HTTP %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			now := time.Now()
			tick := models.CandleTick{InstID: tr.InstID, TimeframeRaw: "1m", Close: price, End: now}
			cached := models.CachedPos{Size: 2, Entry: 105, PosSide: "long"}
			s.trailOne(ctx, tick, cached)
			saved, err := repo.GetByGUID(ctx, tr.GUID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Payload.EntryPrice != 100 || saved.Payload.RiskDist != 10 || saved.Payload.StopLoss != 90 {
				t.Fatal("initial risk changed")
			}
			if mode == "stop failure" {
				if !saved.Payload.ProfitRunnerActive || saved.Payload.CurrentStopLoss != 101 || partialCalls != 0 || len(orders) != 2 {
					t.Fatalf("unsafe failed replacement: %+v", saved.Payload)
				}
				return
			}
			if !saved.Payload.ProfitRunnerActive || saved.Payload.CurrentStopLoss != 130 || saved.Payload.TPAlgoID != "" {
				t.Fatalf("runner not persisted %+v", saved.Payload)
			}
			wantPartial := 0
			if mode == "partial" || mode == "ambiguous partial" {
				wantPartial = 1
			}
			if partialCalls != wantPartial {
				t.Fatalf("partial count %d want %d", partialCalls, wantPartial)
			}
			if mode == "partial" && (!saved.Payload.TookPartial || saved.Payload.CurrentSize != 1 || stopCalls != 2) {
				t.Fatalf("partial residue unprotected %+v calls=%d", saved.Payload, stopCalls)
			}
			// Simulate restart + stale position cache; neither may duplicate a partial.
			s.TrailStates[key] = trailStateFromTrade(*saved)
			price = 145
			tick.Close = 145
			tick.End = now.Add(time.Minute)
			s.trailOne(ctx, tick, cached)
			if partialCalls != wantPartial {
				t.Fatal("partial repeated after restart")
			}
			saved, err = repo.GetByGUID(ctx, tr.GUID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Payload.CurrentStopLoss != 135 {
				t.Fatalf("stop did not follow price %+v", saved.Payload)
			}
			price = 142
			tick.Close = 142
			tick.End = now.Add(2 * time.Minute)
			before := stopCalls
			s.trailOne(ctx, tick, cached)
			if stopCalls != before {
				t.Fatal("replaced or loosened stop on retracement")
			}
		})
	}
}
