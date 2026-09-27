package pg

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
	"trade_bot/pkg/db"
)

func TestEntryRestrictionsPostgres(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	raw, err := os.ReadFile("../../../../migrations/0007_instrument_entry_blocks.sql")
	if err != nil {
		t.Fatal(err)
	}
	// This test owns its temporary table; it never accepts a production DSN.
	if _, err = pool.Exec(ctx, `DROP TABLE IF EXISTS public.instrument_entry_blocks`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, strings.Split(string(raw), "-- +goose Down")[0]); err != nil {
		t.Fatal(err)
	}
	repo := NewUser(db.NewPgTxManager(pool))
	first, err := repo.BlockEntry(ctx, 7, "CL-USDT-SWAP", "51155")
	if err != nil || !first {
		t.Fatalf("insert %v %v", first, err)
	}
	first, err = repo.BlockEntry(ctx, 7, "CL-USDT-SWAP", "51155")
	if err != nil || first {
		t.Fatalf("duplicate %v %v", first, err)
	}
	// Reopen the connection to simulate a process restart, not just a map lookup.
	pool2, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool2.Close()
	restarted := NewUser(db.NewPgTxManager(pool2))
	for _, tc := range []struct {
		user int64
		inst string
		want bool
	}{{7, "CL-USDT-SWAP", true}, {8, "CL-USDT-SWAP", false}, {7, "BTC-USDT-SWAP", false}} {
		got, err := restarted.EntryBlocked(ctx, tc.user, tc.inst)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got %v %v", tc, got, err)
		}
	}
	if _, err = repo.BlockEntry(ctx, 7, "BTC-USDT-SWAP", "51121"); err == nil {
		t.Fatal("size error persisted as restriction")
	}
	cancelled, stop := context.WithCancel(ctx)
	stop()
	if _, err = restarted.EntryBlocked(cancelled, 7, "CL-USDT-SWAP"); err == nil {
		t.Fatal("storage error swallowed")
	}
}
