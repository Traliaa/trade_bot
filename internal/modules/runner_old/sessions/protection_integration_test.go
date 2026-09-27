package sessions

import (
	"context"
	"encoding/json"
	"errors"
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

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type protectionTransport func(*http.Request) (*http.Response, error)

func (f protectionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type protectionNotifier struct{ messages []string }

func (n *protectionNotifier) Send(context.Context, int64, string) (tgbot.Message, error) {
	return tgbot.Message{}, nil
}
func (n *protectionNotifier) SendF(_ context.Context, _ int64, format string, _ ...any) (tgbot.Message, error) {
	n.messages = append(n.messages, format)
	return tgbot.Message{}, nil
}

// Uses real repository SQL plus an in-memory HTTP exchange. Never sends orders.
func TestProtectionSessionPostgres(t *testing.T) {
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
	for _, file := range []string{"0003_add_trade_history.sql", "0007_instrument_entry_blocks.sql"} {
		raw, err := os.ReadFile("../../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		up := strings.Split(string(raw), "-- +goose Down")[0]
		up = strings.ReplaceAll(up, "CREATE TABLE public.instrument_entry_blocks", "CREATE TABLE IF NOT EXISTS public.instrument_entry_blocks")
		if _, err := pool.Exec(ctx, up); err != nil {
			t.Fatal(err)
		}
	}
	repo := pg.NewUser(db.NewPgTxManager(pool))
	user := &models.UserSettings{TelegramID: 921001, Settings: models.Settings{TrailingConfig: models.TrailingConfig{PartialEnabled: true, PartialTriggerR: 1.25, PartialCloseFrac: .5, BETriggerR: 1, BEOffsetR: .1}}}
	inst := "SAFETY-TEST-USDT-SWAP"
	if _, err := repo.BlockEntry(ctx, user.TelegramID, inst, "51155"); err != nil {
		t.Fatal(err)
	}
	s := &UserSession{Base: base.Base{Logger: zap.NewNop()}, User: user, Repo: repo, Okx: okx.NewClient(user), Notifier: &protectionNotifier{}}
	s.LastMsgAt = make(map[string]time.Time)
	s.UpdateSettings(user.Settings)
	if _, err := s.OpenPositionWithTpSl(ctx, models.Signal{InstID: inst}, nil); !errors.Is(err, ErrEntryBlocked) {
		t.Fatalf("entry bypassed block: %v", err)
	}

	id := uuid.New()
	tr := models.TradeRecord{GUID: id, UserID: user.TelegramID, InstID: inst, Status: models.TradeStatusOpen, EntryAt: time.Now(), Payload: models.TradePayload{PosSide: "long", EntryPrice: 100, EntrySize: .03, StopLoss: 90, TakeProfit: 120, RiskDist: 10, AlgoID: "old-sl", TPAlgoID: "tp-live"}}
	tr.CreatedAt = time.Now()
	tr.UpdatedAt = time.Now()
	if err := repo.CreateTradeHistory(ctx, tr); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(ctx, "DELETE FROM public.trade_history WHERE guid=$1", id) }()
	st := trailStateFromTrade(tr)
	s.TrailStates = map[models.PosKey]*models.PositionTrailState{helper.TrailKey(inst, "long"): st}
	previous := http.DefaultTransport
	defer func() { http.DefaultTransport = previous }()
	partialCalls, stopCalls := 0, 0
	http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/api/v5/market/ticker":
			body = `{"code":"0","data":[{"last":"115"}]}`
		case "/api/v5/public/instruments":
			body = `{"code":"0","data":[{"instId":"SAFETY-TEST-USDT-SWAP","ctType":"linear","settleCcy":"USDT","ctValCcy":"SAFETY","lotSz":"0.01","minSz":"0.01","tickSz":"0.01","ctVal":"1"}]}`
		case "/api/v5/trade/order":
			partialCalls++
			var order map[string]any
			if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
				t.Fatal(err)
			}
			if order["sz"] != "0.01" || order["reduceOnly"] != true {
				t.Fatalf("invalid partial: %v", order)
			}
			body = `{"code":"1","data":[{"sCode":"51121","sMsg":"rejected"}]}`
		case "/api/v5/trade/order-algo":
			stopCalls++
			body = `{"code":"0","data":[{"algoId":"new-sl","sCode":"0"}]}`
		case "/api/v5/trade/cancel-algos":
			body = `{"code":"0","data":[{"algoId":"old-sl","sCode":"0"}]}`
		default:
			t.Fatalf("unexpected network call: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	s.trailOne(ctx, models.CandleTick{InstID: inst, High: 115, Low: 114, Close: 115, End: time.Now()}, models.CachedPos{Size: .03, Entry: 100, PosSide: "long"})
	if partialCalls != 1 || stopCalls != 1 || !st.MovedToBE || st.TookPartial {
		t.Fatalf("partial prevented protection: partial=%d stop=%d state=%+v", partialCalls, stopCalls, st)
	}
	saved, err := repo.GetByGUID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	restored := trailStateFromTrade(*saved)
	if restored.AlgoID != "new-sl" || restored.SL != 101 || restored.TPAlgoID != "tp-live" || saved.Payload.StopLoss != 90 {
		t.Fatalf("protection not persisted: %+v", saved.Payload)
	}
	s.checkV3PartialForSide(ctx, models.CandleTick{InstID: inst, End: time.Now().Add(time.Minute)}, "long")
	if partialCalls != 2 {
		t.Fatal("V3 partial was not exercised")
	}
	st.Size = .01
	s.checkV3PartialForSide(ctx, models.CandleTick{InstID: inst, End: time.Now().Add(2 * time.Minute)}, "long")
	if partialCalls != 2 {
		t.Fatal("V3 submitted an impossible partial")
	}
}
