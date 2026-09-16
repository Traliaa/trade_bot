package pg

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/pkg/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReconciliationPostgresSnapshot(t *testing.T) {
	dsn := os.Getenv("TRADE_REPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("set TRADE_REPORT_TEST_DSN to an isolated local trade_report_test database")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "trade_report_test" {
		t.Fatal("integration test only accepts an isolated loopback trade_report_test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, path := range []string{"../../../../migrations/0003_add_trade_history.sql", "../../../../migrations/0004_add_trade_fills.sql"} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, strings.Split(string(raw), "-- +goose Down")[0]); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewUser(db.NewPgTxManager(pool))
	userID := time.Now().UnixNano()
	at := time.Now().UTC().Add(-time.Hour)
	end := at.Add(time.Minute)
	var verifiedID uuid.UUID
	for i := 0; i < 4; i++ {
		tr := models.TradeRecord{GUID: uuid.New(), UserID: userID, InstID: "ETH-USDT-SWAP", Strategy: "fixture", Timeframe: "15m", Status: models.TradeStatusClosed, EntryAt: at, ExitAt: &end,
			Payload: models.TradePayload{PosSide: "long", EntrySize: 1, ExitSize: 1, TotalFees: -.2, GrossRealizedPnL: 2, RealizedPnL: 1.8}}
		if i == 2 {
			tr.Status = models.TradeStatusOpen
			tr.ExitAt = nil
			tr.Payload.UnrealizedPnL = .5
		}
		if i == 3 {
			tr.UserID++
		}
		raw, _ := tr.Payload.Marshal()
		_, err := pool.Exec(ctx, `INSERT INTO public.trade_history(guid,user_id,inst_id,strategy,timeframe,status,entry_at,exit_at,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tr.GUID, tr.UserID, tr.InstID, tr.Strategy, tr.Timeframe, tr.Status, tr.EntryAt, tr.ExitAt, raw)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 || i == 3 {
			if i == 0 {
				verifiedID = tr.GUID
			}
			err := repo.UpsertTradeFills(ctx, []models.TradeFillRecord{
				{TradeGUID: tr.GUID, TradeID: "entry", InstID: tr.InstID, PosSide: "long", Side: "buy", Role: models.TradeFillRoleEntry, FillPrice: 100, FillSize: 1, Fee: -.1, FilledAt: at},
				{TradeGUID: tr.GUID, TradeID: "exit", InstID: tr.InstID, PosSide: "long", Side: "sell", Role: models.TradeFillRoleExit, FillPrice: 102, FillSize: 1, Fee: -.1, RealizedPnL: 2, FilledAt: end},
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	stats, err := repo.GetTradeStats(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	r := stats.Reconciliation
	if r == nil {
		t.Fatal("stats omitted reconciliation")
	}
	if stats.TotalTrades != 3 || stats.OpenTrades != 1 || stats.ClosedTrades != 2 || stats.OpenPnL != .5 || r.Verified != 1 || r.Incomplete != 1 || r.Mismatched != 0 || math.Abs(r.VerifiedStats.NetPnL-1.8) > 1e-12 {
		t.Fatalf("inconsistent snapshot or other user's data leaked: %+v / %+v", stats, r)
	}
	if r.FundingStatus != "not_checked" || r.Scope != "stored_fills" || time.Since(r.CheckedAt) > time.Minute {
		t.Fatalf("bad evidence scope: %+v", r)
	}
	// Reconciliation is read-only; it must not backfill payload or close reason.
	tr, err := repo.GetByGUID(ctx, verifiedID)
	if err != nil || tr.CloseReason != models.CloseReasonUnknown || tr.Payload.CloseReasonSource != "" {
		t.Fatalf("report mutated history: %+v %v", tr, err)
	}
	// PG permits nonfinite double precision values; one corrupt fill must not
	// prevent healthy history from being reported.
	if _, err := pool.Exec(ctx, `UPDATE public.trade_fills SET fee='NaN'::float8 WHERE trade_guid=$1 AND trade_id='exit'`, verifiedID); err != nil {
		t.Fatal(err)
	}
	stats, err = repo.GetTradeStats(ctx, userID)
	if err != nil {
		t.Fatalf("invalid fill disabled report: %v", err)
	}
	if stats.Reconciliation.Mismatched != 1 || stats.Reconciliation.Incomplete != 1 || stats.Reconciliation.Verified != 0 {
		t.Fatalf("invalid fill not isolated: %+v", stats.Reconciliation)
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err := repo.GetTradeStats(cancelled, userID); err == nil {
		t.Fatal("storage failure reported as empty success")
	}
}
