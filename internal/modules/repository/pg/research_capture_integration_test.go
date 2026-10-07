package pg

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
	"trade_bot/internal/models"
	"trade_bot/pkg/db"
)

func researchTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TRADE_REPORT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated loopback trade_report_test")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "trade_report_test" {
		t.Fatal("local trade_report_test only")
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal("test pool unavailable")
	}
	t.Cleanup(pool.Close)
	for _, file := range []string{"0003_add_trade_history.sql", "0008_add_research_entry_snapshot.sql"} {
		raw, err := os.ReadFile("../../../../migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(context.Background(), strings.Split(string(raw), "-- +goose Down")[0]); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

func TestResearchSnapshotPostgresCompatibility(t *testing.T) {
	pool := researchTestPool(t)
	ctx := context.Background()
	repo := NewUser(db.NewPgTxManager(pool))
	at := time.Now().UTC()
	old := uuid.New()
	defer pool.Exec(ctx, "DELETE FROM public.trade_history WHERE guid=$1", old)
	// Literal pre-migration INSERT is intentionally independent of regenerated SQL.
	_, err := pool.Exec(ctx, `INSERT INTO public.trade_history(guid,user_id,inst_id,strategy,timeframe,status,entry_at,payload) VALUES($1,1,'TEST-USDT-SWAP','test','15m','open',$2,'{}')`, old, at)
	if err != nil {
		t.Fatal(err)
	}
	var isNull bool
	if err = pool.QueryRow(ctx, "SELECT research_entry_snapshot IS NULL FROM public.trade_history WHERE guid=$1", old).Scan(&isNull); err != nil || !isNull {
		t.Fatal("old INSERT incompatible")
	}
	id := uuid.New()
	defer pool.Exec(ctx, "DELETE FROM public.trade_history WHERE guid=$1", id)
	tr := models.TradeRecord{GUID: id, UserID: 1, InstID: "TEST-USDT-SWAP", Strategy: "test", Timeframe: "15m", Status: models.TradeStatusOpen, EntryAt: at, CreatedAt: at, UpdatedAt: at, ResearchEntrySnapshot: json.RawMessage(`{"snapshot":"original","zero":0,"enabled":false}`)}
	if err = repo.CreateTradeHistory(ctx, tr); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if err = pool.QueryRow(ctx, "SELECT research_entry_snapshot::text FROM public.trade_history WHERE guid=$1", id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = repo.UpdatePayload(ctx, id, models.TradePayload{EntryPrice: 100}); err != nil {
		t.Fatal(err)
	}
	if err = repo.CloseTrade(ctx, id, models.TradeCloseInput{ExitAt: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT research_entry_snapshot::text FROM public.trade_history WHERE guid=$1", id).Scan(&after); err != nil || before != after {
		t.Fatal("snapshot overwritten")
	}
	tr.GUID = uuid.New()
	tr.ResearchEntrySnapshot = nil
	defer pool.Exec(ctx, "DELETE FROM public.trade_history WHERE guid=$1", tr.GUID)
	if err = repo.CreateTradeHistory(ctx, tr); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT research_entry_snapshot IS NULL FROM public.trade_history WHERE guid=$1", tr.GUID).Scan(&isNull); err != nil || !isNull {
		t.Fatal("nil must map to SQL NULL")
	}
}

func BenchmarkResearchSnapshotInsert(b *testing.B) {
	pool := researchTestPool(b)
	ctx := context.Background()
	repo := NewUser(db.NewPgTxManager(pool))
	for _, tc := range []struct {
		name string
		size int
	}{{"null", 0}, {"representative", 4096}, {"near_limit", 32740}} {
		b.Run(tc.name, func(b *testing.B) {
			var raw json.RawMessage
			if tc.size > 0 {
				raw = json.RawMessage(`{"data":"` + strings.Repeat("x", tc.size) + `"}`)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			for i := 0; i < b.N; i++ {
				at := time.Now()
				tr := models.TradeRecord{GUID: uuid.New(), UserID: 1, InstID: "BENCH-USDT-SWAP", Strategy: "test", Timeframe: "15m", Status: models.TradeStatusOpen, EntryAt: at, CreatedAt: at, UpdatedAt: at, ResearchEntrySnapshot: raw}
				if err := repo.CreateTradeHistory(ctx, tr); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				if _, err := pool.Exec(ctx, "DELETE FROM public.trade_history WHERE guid=$1", tr.GUID); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
		})
	}
}
