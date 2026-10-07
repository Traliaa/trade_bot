package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	okx "trade_bot/internal/modules/okx_client/service"
	"trade_bot/internal/modules/repository/pg"
	"trade_bot/pkg/db"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type closeCountingNotifier struct{ sends int }

func (n *closeCountingNotifier) Send(context.Context, int64, string) (tgbot.Message, error) {
	n.sends++
	return tgbot.Message{}, nil
}
func (n *closeCountingNotifier) SendF(context.Context, int64, string, ...any) (tgbot.Message, error) {
	n.sends++
	return tgbot.Message{}, nil
}

func TestCloseExecutionPostgresRetry(t *testing.T) {
	dsn := os.Getenv("TRADE_REPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated loopback trade_report_test")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "trade_report_test" {
		t.Fatal("local test DB only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, file := range []string{"0003_add_trade_history.sql", "0004_add_trade_fills.sql", "0008_add_research_entry_snapshot.sql"} {
		raw, err := os.ReadFile("../../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, strings.Split(string(raw), "-- +goose Down")[0]); err != nil {
			t.Fatal(err)
		}
	}
	for _, scenario := range []string{"retry_close_write", "duplicate", "excess", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			repo := pg.NewUser(db.NewPgTxManager(pool))
			user := &models.UserSettings{TelegramID: time.Now().UnixNano(), Settings: models.Settings{TradingSettings: models.TradingSettings{OKXAPIKey: "fixture", OKXAPISecret: "fixture", OKXPassphrase: "fixture"}}}
			at := time.UnixMilli(1791184503030)
			tr := models.TradeRecord{GUID: uuid.New(), UserID: user.TelegramID, InstID: "ARB-USDT-SWAP", Status: models.TradeStatusOpen, EntryAt: at, CreatedAt: at, UpdatedAt: at,
				Payload: models.TradePayload{PosSide: "long", EntrySize: 1.8, EntryPrice: .208, TotalFees: -.002, PendingCloseReason: "time_stop", PlannedRiskUSDT: .1}}
			if err := repo.CreateTradeHistory(ctx, tr); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM trade_history WHERE guid=$1", tr.GUID) })
			rows := arbCloseRows()
			switch scenario {
			case "duplicate":
				rows = append(rows, rows[0])
			case "excess":
				rows[3]["fillSz"] = "0.100000000000000001"
			case "conflict":
				rows[1]["tradeId"] = rows[0]["tradeId"]
			}
			body, err := json.Marshal(map[string]any{"code": "0", "data": rows})
			if err != nil {
				t.Fatal(err)
			}
			old := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = old })
			calls := 0
			http.DefaultTransport = protectionTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet {
					t.Fatalf("unexpected exchange mutation %s", r.Method)
				}
				response := `{"code":"0","data":[]}`
				switch r.URL.Path {
				case "/api/v5/account/positions":
				case "/api/v5/trade/fills":
					response = string(body)
				default:
					t.Fatalf("unexpected request %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			notifier := &closeCountingNotifier{}
			s := &UserSession{Base: base.Base{Logger: zap.NewNop()}, User: user, Repo: repo, Okx: okx.NewClient(user), Notifier: notifier}
			if scenario == "retry_close_write" {
				// Fail only the closing UPDATE of this fixture after the fills INSERT commits.
				_, err := pool.Exec(ctx, fmt.Sprintf(`CREATE FUNCTION public.fixture_reject_close() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.guid='%s'::uuid AND NEW.status='closed' THEN RAISE EXCEPTION 'fixture close failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER fixture_reject_close BEFORE UPDATE ON trade_history FOR EACH ROW EXECUTE FUNCTION public.fixture_reject_close();`, tr.GUID))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, _ = pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS fixture_reject_close ON trade_history; DROP FUNCTION IF EXISTS public.fixture_reject_close();")
				})
				if err := s.SyncClosedTrades(ctx); err != nil {
					t.Fatal(err)
				}
				pending, err := repo.GetByGUID(ctx, tr.GUID)
				if err != nil {
					t.Fatal(err)
				}
				fills, err := repo.ListTradeFills(ctx, tr.GUID)
				if err != nil {
					t.Fatal(err)
				}
				if pending.Status != models.TradeStatusOpen || pending.Payload.TotalFees != -.002 || len(fills) != 4 || notifier.sends != 0 {
					t.Fatal("partial write was not retryable")
				}
				if _, err = pool.Exec(ctx, "DROP TRIGGER fixture_reject_close ON trade_history; DROP FUNCTION public.fixture_reject_close();"); err != nil {
					t.Fatal(err)
				}
			}
			var workers sync.WaitGroup
			errs := make(chan error, 2)
			for i := 0; i < 2; i++ {
				workers.Add(1)
				go func() { defer workers.Done(); errs <- s.SyncClosedTrades(ctx) }()
			}
			workers.Wait()
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			got, err := repo.GetByGUID(ctx, tr.GUID)
			if err != nil {
				t.Fatal(err)
			}
			fills, err := repo.ListTradeFills(ctx, tr.GUID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "excess" || scenario == "conflict" {
				if got.Status != models.TradeStatusOpen || len(fills) != 0 || notifier.sends != 0 {
					t.Fatal("unsafe close persisted")
				}
				return
			}
			if got.Status != models.TradeStatusClosed || got.Payload.ExitSize != 1.8 || len(fills) != 4 || notifier.sends != 1 || math.Abs(got.Payload.TotalFees-(-.006)) > 1e-15 || math.Abs(got.Payload.RealizedPnL-(-.046)) > 1e-15 {
				t.Fatalf("retry changed accounting: %+v fills=%d notifications=%d", got.Payload, len(fills), notifier.sends)
			}
			before := calls
			cancelled, stop := context.WithCancel(ctx)
			stop()
			if s.SyncClosedTrades(cancelled) == nil || calls != before {
				t.Fatal("cancelled sync did not stop before exchange")
			}
		})
	}
}
