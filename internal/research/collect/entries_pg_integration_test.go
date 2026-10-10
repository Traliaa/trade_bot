package collect

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"trade_bot/internal/models"
)

type entryObservedDB struct {
	pool          *pgxpool.Pool
	t             *testing.T
	afterSnapshot func()
}

func (d entryObservedDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := d.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	var isolation, readOnly string
	var count int
	// Establish the DB snapshot before the concurrent update; this is a DB-only
	// test hook, never a production callback or an external network operation.
	err = tx.QueryRow(ctx, `SELECT current_setting('transaction_isolation'), current_setting('transaction_read_only'), (SELECT count(*) FROM research_collect.entries_v1)`).Scan(&isolation, &readOnly, &count)
	if err != nil {
		_ = tx.Rollback(ctx)
		d.t.Fatal("cannot inspect isolated transaction")
	}
	if isolation != "repeatable read" || readOnly != "on" {
		_ = tx.Rollback(ctx)
		d.t.Fatal("adapter did not request read-only repeatable-read")
	}
	if d.afterSnapshot != nil {
		d.afterSnapshot()
	}
	return tx, nil
}

func TestEntriesPostgresRestrictedSnapshot(t *testing.T) {
	dsn := os.Getenv("RESEARCH_COLLECT_TEST_DSN")
	if dsn == "" {
		t.Skip("requires dedicated loopback trade_report_test")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Database != "trade_report_test" || len(cfg.ConnConfig.Fallbacks) > 0 {
		t.Fatal("dedicated loopback DB without fallbacks only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("test DB unavailable")
	}
	defer admin.Close()
	// No IF EXISTS/replace: fail on collisions rather than touch someone else's
	// test fixtures. This database must be disposable and separately provisioned.
	if _, err = admin.Exec(ctx, `CREATE SCHEMA research_collect;
 CREATE TABLE research_collect.fixture_entries(row_id uuid primary key, account int, inst_id text, status text, entry_at timestamptz, open_order_id text, research_entry_snapshot jsonb);
 CREATE VIEW research_collect.entries_v1 WITH (security_barrier=true) AS SELECT row_id, inst_id, status, entry_at, open_order_id, research_entry_snapshot FROM research_collect.fixture_entries WHERE account=1;
 CREATE TABLE research_collect.fixture_links(row_id uuid, order_id text, role text);
 CREATE VIEW research_collect.order_links_v1 WITH (security_barrier=true) AS SELECT f.row_id,f.order_id,f.role FROM research_collect.fixture_links f JOIN research_collect.fixture_entries e ON e.row_id=f.row_id WHERE e.account=1;
 CREATE ROLE research_collect_test_reader LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT;
 GRANT USAGE ON SCHEMA research_collect TO research_collect_test_reader;
 GRANT SELECT ON research_collect.entries_v1,research_collect.order_links_v1 TO research_collect_test_reader;`); err != nil {
		t.Fatal("fixture setup failed")
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		p, err := pgxpool.NewWithConfig(cleanup, cfg.Copy())
		if err != nil {
			t.Error("fixture cleanup connection failed")
			return
		}
		defer p.Close()
		if _, err = p.Exec(cleanup, `DROP VIEW research_collect.order_links_v1; DROP TABLE research_collect.fixture_links; DROP VIEW research_collect.entries_v1; DROP TABLE research_collect.fixture_entries; DROP SCHEMA research_collect; DROP ROLE research_collect_test_reader`); err != nil {
			t.Error("fixture cleanup failed")
		}
	})
	_, w := entriesFixture(t)
	base := snapshotFixture(t, false)
	input, _, err := DecodeSnapshot(base, "study")
	if err != nil {
		t.Fatal(err)
	}
	// PostgreSQL loses sub-microsecond timestamp precision, JSONB does not.
	input.EntryAt = input.EntryAt.Add(123 * time.Nanosecond)
	raw, code := models.BuildResearchEntrySnapshot(input)
	if code != "" {
		t.Fatal(code)
	}
	id := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	insert := func(id uuid.UUID, account int, at time.Time, snapshot any, order string) {
		t.Helper()
		if _, err := admin.Exec(ctx, `INSERT INTO research_collect.fixture_entries VALUES($1,$2,'ETH-USDT-SWAP','open',$3,$4,$5)`, id, account, at, order, snapshot); err != nil {
			t.Fatal("fixture insert failed")
		}
	}
	insert(id, 1, input.EntryAt, raw, "private-entry-order")
	insert(uuid.New(), 2, input.EntryAt, raw, "other-account-order")
	insert(uuid.New(), 1, w.To, raw, "outside-window-order")
	readerCfg := cfg.Copy()
	readerCfg.ConnConfig.User = "research_collect_test_reader"
	readerCfg.ConnConfig.Password = ""
	reader, err := pgxpool.NewWithConfig(ctx, readerCfg)
	if err != nil {
		t.Fatal("reader unavailable")
	}
	defer reader.Close()
	for _, sql := range []string{`SELECT * FROM research_collect.fixture_entries`, `UPDATE research_collect.entries_v1 SET status='closed'`} {
		if _, err := reader.Exec(ctx, sql); err == nil {
			t.Fatal("restricted role can access base table or write")
		}
	}
	b, err := readStudyEntries(ctx, entryObservedDB{reader, t, func() {
		if _, err := admin.Exec(ctx, `UPDATE research_collect.fixture_entries SET status='closed' WHERE row_id=$1`, id); err != nil {
			t.Fatal("concurrent fixture update failed")
		}
		if _, err := admin.Exec(ctx, `INSERT INTO research_collect.fixture_links VALUES($1,'private-exit-order','exit')`, id); err != nil {
			t.Fatal("concurrent link failed")
		}
	}}, w)
	if err != nil || b.Coverage.Rows != 1 || b.Coverage.Complete != 1 || b.entries[0].state != "open" || len(b.entries[0].exitOrders) != 0 || !b.entries[0].linksComplete {
		t.Fatalf("snapshot/isolation/scope failed: %+v %v", b.Coverage, err)
	}
	b, err = ReadStudyEntries(ctx, reader, w)
	if err != nil || b.entries[0].state != "closed" || len(b.entries[0].exitOrders) != 1 {
		t.Fatal("next pass did not observe committed state")
	}
	// A To bound one nanosecond after this DB row used to be truncated by pgx,
	// excluding the row yet reporting a complete empty batch. Reject that bound.
	narrow := w
	narrow.To = input.EntryAt.Truncate(time.Microsecond).Add(time.Nanosecond)
	if _, err := ReadEntries(ctx, reader, narrow); err == nil {
		t.Fatal("unrepresentable time bound silently accepted")
	}
	// Oversize/corrupt input stays bounded and distinct from SQL NULL.
	oversize, _ := json.Marshal(map[string]string{"PRIVATE": strings.Repeat("x", 40000)})
	insert(uuid.New(), 1, input.EntryAt.Add(time.Second), oversize, strings.Repeat("x", 200))
	insert(uuid.New(), 1, input.EntryAt.Add(2*time.Second), nil, "")
	b, err = ReadEntries(ctx, reader, w)
	if err != nil || b.Coverage.Rows != 3 || b.Coverage.Invalid != 1 || b.Coverage.Missing != 1 || b.Coverage.AttributionPending != 2 {
		t.Fatalf("size/null coverage failed: %+v %v", b.Coverage, err)
	}
	if b.entries[1].orderID != "" || b.entries[1].capture != nil {
		t.Fatal("oversized raw evidence transferred")
	}
	w.Limit = 1
	b, err = ReadEntries(ctx, reader, w)
	if err != nil || !b.Coverage.Truncated || b.Coverage.Rows != 1 {
		t.Fatal("overflow not reported")
	}
	ctxCancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := ReadEntries(ctxCancelled, reader, w); err == nil {
		t.Fatal("cancelled read accepted")
	}
}
