package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Only the external pgx boundary is replaced; transaction/query orchestration,
// normalization, snapshot validation, privacy and coverage run unchanged.
type entryWireRow struct {
	id            uuid.UUID
	symbol, state string
	at            time.Time
	order         string
	present       bool
	raw           *string
}
type entryTestRows struct {
	pgx.Rows
	rows         []entryWireRow
	index        int
	closed       bool
	err, scanErr error
}

func (r *entryTestRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}
func (r *entryTestRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	v := r.rows[r.index-1]
	*(dest[0].(*uuid.UUID)) = v.id
	*(dest[1].(*string)) = v.symbol
	*(dest[2].(*string)) = v.state
	*(dest[3].(*time.Time)) = v.at
	*(dest[4].(*string)) = v.order
	*(dest[5].(*bool)) = v.present
	*(dest[6].(**string)) = v.raw
	return nil
}
func (r *entryTestRows) Err() error { return r.err }
func (r *entryTestRows) Close()     { r.closed = true }

type entryTestTx struct {
	pgx.Tx
	t                 *testing.T
	rows              *entryTestRows
	committed, rolled bool
	fail              string
	window            EntryWindow
	cancel            context.CancelFunc
}

func (x *entryTestTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if sql != "SET LOCAL statement_timeout = '5s'; SET LOCAL lock_timeout = '1s'" || len(args) != 0 {
		x.t.Fatal("unexpected transaction mutation", sql)
	}
	if x.fail == "setup" {
		return pgconn.CommandTag{}, errors.New("PRIVATE setup")
	}
	return pgconn.CommandTag{}, nil
}
func (x *entryTestTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !strings.Contains(sql, "FROM research_collect.entries_v1") || strings.Contains(sql, "user_settings") || strings.Contains(sql, "SELECT *") || !strings.Contains(sql, "ORDER BY entry_at, row_id") {
		x.t.Fatal("unrestricted query", sql)
	}
	if len(args) != 3 || args[0] != x.window.From.UTC() || args[1] != x.window.To.UTC() || args[2] != x.window.Limit+1 {
		x.t.Fatal("wrong inclusion window/overflow probe")
	}
	if x.fail == "query" {
		return nil, errors.New("PRIVATE query")
	}
	if x.cancel != nil {
		x.cancel()
	}
	return x.rows, nil
}
func (x *entryTestTx) Commit(ctx context.Context) error {
	if !x.rows.closed {
		x.t.Fatal("commit before rows closed")
	}
	x.committed = true
	if x.fail == "commit" {
		return errors.New("PRIVATE commit")
	}
	return nil
}
func (x *entryTestTx) Rollback(ctx context.Context) error {
	if ctx.Err() != nil {
		x.t.Fatal("cleanup context already cancelled")
	}
	x.rolled = true
	return nil
}

type entryTestDB struct {
	tx     *entryTestTx
	begins int
	fail   bool
}

func (d *entryTestDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	d.begins++
	if opts.IsoLevel != pgx.RepeatableRead || opts.AccessMode != pgx.ReadOnly {
		d.tx.t.Fatal("unsafe transaction")
	}
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second {
		d.tx.t.Fatal("missing bounded deadline")
	}
	if d.fail {
		return nil, errors.New("PRIVATE begin")
	}
	return d.tx, nil
}
func entriesFixture(t *testing.T) (*entryTestDB, EntryWindow) {
	t.Helper()
	w := EntryWindow{From: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Protocol: "study", Limit: 10}
	raw := string(snapshotFixture(t, false))
	return &entryTestDB{tx: &entryTestTx{t: t, window: w, rows: &entryTestRows{rows: []entryWireRow{{uuid.MustParse("22222222-2222-4222-8222-222222222222"), "ETH-USDT-SWAP", "open", time.Date(2026, 10, 8, 10, 1, 2, 3000000, time.UTC), "PRIVATE-order", true, &raw}}}}}, w
}
func TestEntriesReadAndPrivacy(t *testing.T) {
	db, w := entriesFixture(t)
	b, err := readEntries(context.Background(), db, w)
	if err != nil || b.Coverage.Rows != 1 || b.Coverage.Complete != 1 || b.Coverage.Truncated || len(b.entries) != 1 || !db.tx.committed || !db.tx.rows.closed {
		t.Fatalf("unexpected batch: %+v %v", b.Coverage, err)
	}
	for _, v := range []any{b, b.entries[0]} {
		if _, err := json.Marshal(v); err == nil {
			t.Fatal("private batch serialized")
		}
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
			out := fmt.Sprintf(format, v)
			if strings.Contains(out, "PRIVATE") || strings.Contains(out, "22222222") {
				t.Fatal("private evidence formatted")
			}
		}
	}
	safe, err := json.Marshal(b.Coverage)
	if err != nil || strings.Contains(string(safe), "PRIVATE") {
		t.Fatal("unsafe coverage")
	}
}
func TestEntriesCoverage(t *testing.T) {
	for _, kind := range []string{"missing", "invalid", "oversize", "symbol", "timestamp", "protocol", "incomplete", "missing_order", "duplicate_capture", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			db, w := entriesFixture(t)
			row := &db.tx.rows.rows[0]
			switch kind {
			case "missing":
				row.present = false
				row.raw = nil
			case "invalid":
				bad := "PRIVATE malformed JSON"
				row.raw = &bad
			case "oversize":
				row.raw = nil
			case "symbol":
				row.symbol = "BTC-USDT-SWAP"
			case "timestamp":
				row.at = row.at.Add(time.Minute)
			case "protocol":
				w.Protocol = "different"
			case "incomplete":
				raw := string(snapshotFixture(t, true))
				row.raw = &raw
			case "missing_order":
				row.order = ""
			case "duplicate_capture", "overflow":
				other := *row
				other.id = uuid.MustParse("33333333-3333-4333-8333-333333333333")
				db.tx.rows.rows = append(db.tx.rows.rows, other)
				if kind == "overflow" {
					w.Limit = 1
				}
			}
			db.tx.window = w
			b, err := readEntries(context.Background(), db, w)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing":
				if b.Coverage.Missing != 1 {
					t.Fatal(b.Coverage)
				}
			case "incomplete":
				if b.Coverage.Incomplete != 1 {
					t.Fatal(b.Coverage)
				}
			case "missing_order":
				if b.Coverage.AttributionPending != 1 || b.Coverage.Complete != 1 {
					t.Fatal(b.Coverage)
				}
			case "duplicate_capture":
				if b.Coverage.Invalid != 2 || b.Coverage.Complete != 0 {
					t.Fatal(b.Coverage)
				}
			case "overflow":
				if !b.Coverage.Truncated || b.Coverage.Rows != 1 || len(b.entries) != 1 {
					t.Fatal(b.Coverage)
				}
			default:
				if b.Coverage.Invalid != 1 || b.Coverage.Complete != 0 {
					t.Fatal(b.Coverage)
				}
			}
		})
	}
}
func TestEntriesFailureCleanup(t *testing.T) {
	for _, kind := range []string{"begin", "setup", "query", "scan", "iteration", "commit", "cancel", "duplicate_row", "outside_window"} {
		t.Run(kind, func(t *testing.T) {
			db, w := entriesFixture(t)
			db.tx.fail = kind
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch kind {
			case "begin":
				db.fail = true
			case "scan":
				db.tx.rows.scanErr = errors.New("PRIVATE scan")
			case "iteration":
				db.tx.rows.err = errors.New("PRIVATE iteration")
			case "cancel":
				db.tx.cancel = cancel
			case "duplicate_row":
				db.tx.rows.rows = append(db.tx.rows.rows, db.tx.rows.rows[0])
			case "outside_window":
				db.tx.rows.rows[0].at = w.To
			}
			b, err := readEntries(ctx, db, w)
			if err == nil || strings.Contains(err.Error(), "PRIVATE") || len(b.entries) > 0 || b.Coverage.Rows != 0 {
				t.Fatal("failure leaked partial/unsafe evidence")
			}
			if kind != "begin" && !db.tx.rolled {
				t.Fatal("transaction leaked")
			}
			if kind != "begin" && kind != "setup" && kind != "query" && !db.tx.rows.closed {
				t.Fatal("rows leaked")
			}
		})
	}
}
func TestEntriesInvalidWindowAndNilPool(t *testing.T) {
	if _, err := ReadEntries(context.Background(), nil, EntryWindow{}); err == nil {
		t.Fatal("nil pool accepted")
	}
	for _, kind := range []string{"zero", "reverse", "wide", "limit_zero", "limit_large", "protocol", "cancel", "submicro_from", "submicro_to"} {
		db, w := entriesFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		switch kind {
		case "zero":
			w.From = time.Time{}
		case "reverse":
			w.To = w.From
		case "wide":
			w.To = w.From.Add(29 * 24 * time.Hour)
		case "limit_zero":
			w.Limit = 0
		case "limit_large":
			w.Limit = 1001
		case "protocol":
			w.Protocol = ""
		case "cancel":
			cancel()
		case "submicro_from":
			w.From = w.From.Add(time.Nanosecond)
		case "submicro_to":
			w.To = w.To.Add(time.Nanosecond)
		}
		db.tx.window = w
		_, err := readEntries(ctx, db, w)
		cancel()
		if err == nil || db.begins != 0 {
			t.Fatal("invalid request reached DB", kind)
		}
	}
}
