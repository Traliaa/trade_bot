//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func linkedFixture(t *testing.T) (entryRecord, FillArchive, FillWindow) {
	in, _, err := DecodeSnapshot(snapshotFixture(t, false), "study")
	if err != nil {
		t.Fatal(err)
	}
	e := entryRecord{symbol: in.Symbol, state: "closed", entryAt: in.EntryAt, capture: &in, captureStatus: "complete", orderID: "100", exitOrders: []string{"200"}, linksComplete: true}
	w := FillWindow{Symbol: e.symbol, From: e.entryAt.Add(-time.Second), To: e.entryAt.Add(time.Hour), MaxPages: 200}
	entry := Fill{ID: "501", OrderID: "100", Symbol: e.symbol, Side: "buy", PosSide: "long", At: e.entryAt, Size: "2", Price: "100", Fee: "-0.02", PnL: "0", FeeCurrency: "USDT"}
	exit := Fill{ID: "502", OrderID: "200", Symbol: e.symbol, Side: "sell", PosSide: "long", At: e.entryAt.Add(time.Minute), Size: "2", Price: "105", Fee: "-0.02", PnL: "10", FeeCurrency: "USDT"}
	return e, FillArchive{Coverage: FillCoverage{Status: "complete", RecordWindowComplete: true}, fills: []privateFill{{entry, entry.At}, {exit, exit.At}}}, w
}
func TestAttributionExplicitOrders(t *testing.T) {
	e, a, w := linkedFixture(t)
	other := a.fills[1]
	other.Fill.OrderID = "PRIVATE-foreign"
	a.fills = append(a.fills, other)
	r := attributeEntry(e, a, w)
	if r.Reconciliation != "matched" || r.TradingNet == nil || *r.TradingNet != "9.96" || r.Net != nil || r.Costs != "sensitivity-only" {
		t.Fatalf("wrong evidence %+v", r)
	}
	b, _ := json.Marshal(r)
	if strings.Contains(string(b), "PRIVATE") {
		t.Fatal("private attribution leaked")
	}
	for _, kind := range []string{"links", "archive", "missing_exit", "size", "early_window", "open", "time_source"} {
		t.Run(kind, func(t *testing.T) {
			e, a, w := linkedFixture(t)
			switch kind {
			case "links":
				e.linksComplete = false
			case "archive":
				a.Coverage.RecordWindowComplete = false
			case "missing_exit":
				e.exitOrders = nil
			case "size":
				a.fills[1].Fill.Size = "1"
			case "early_window":
				w.From = e.entryAt.Add(time.Second)
			case "open":
				e.state = "open"
			case "time_source":
				e.capture.Evidence.TimeSource = "local_fallback"
			}
			r := attributeEntry(e, a, w)
			if r.TradingNet != nil || r.Net != nil || r.Reconciliation == "matched" {
				t.Fatal("uncertain net certified", r)
			}
		})
	}
}

type linkWire struct {
	row         uuid.UUID
	order, role string
}
type linkRows struct {
	pgx.Rows
	values []linkWire
	n      int
}

func (r *linkRows) Next() bool {
	if r.n >= len(r.values) {
		return false
	}
	r.n++
	return true
}
func (r *linkRows) Scan(v ...any) error {
	x := r.values[r.n-1]
	*v[0].(*uuid.UUID) = x.row
	*v[1].(*string) = x.order
	*v[2].(*string) = x.role
	return nil
}
func (r *linkRows) Close()     {}
func (r *linkRows) Err() error { return nil }

type studyTx struct {
	*entryTestTx
	links   []linkWire
	queried bool
}

func (x *studyTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if strings.Contains(sql, "order_links_v1") {
		if x.committed || !x.rows.closed || len(args) != 1 {
			x.t.Fatal("links outside same snapshot")
		}
		x.queried = true
		return &linkRows{values: x.links}, nil
	}
	return x.entryTestTx.Query(ctx, sql, args...)
}

type studyDB struct {
	*entryTestDB
	study *studyTx
}

func (d *studyDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	if _, err := d.entryTestDB.BeginTx(ctx, o); err != nil {
		return nil, err
	}
	return d.study, nil
}
func TestStudyEntryLinksSameSnapshot(t *testing.T) {
	for _, kind := range []string{"valid", "conflict", "foreign", "overflow"} {
		t.Run(kind, func(t *testing.T) {
			base, w := entriesFixture(t)
			base.tx.rows.rows[0].order = "100"
			id := base.tx.rows.rows[0].id
			links := []linkWire{{id, "100", "entry"}, {id, "200", "exit"}, {id, "200", "exit"}}
			switch kind {
			case "conflict":
				links = append(links, linkWire{id, "100", "exit"})
			case "foreign":
				links = append(links, linkWire{uuid.New(), "300", "exit"})
			case "overflow":
				for len(links) < 10001 {
					links = append(links, linkWire{id, "200", "exit"})
				}
			}
			tx := &studyTx{entryTestTx: base.tx, links: links}
			db := &studyDB{entryTestDB: base, study: tx}
			b, err := readStudyEntries(context.Background(), db, w)
			if kind == "foreign" {
				if err == nil {
					t.Fatal("foreign link accepted")
				}
				return
			}
			if err != nil || !tx.queried || !tx.committed {
				t.Fatal("failed DB pass", err)
			}
			if kind == "valid" {
				if !b.entries[0].linksComplete || len(b.entries[0].exitOrders) != 1 {
					t.Fatal("lost links")
				}
			} else if b.entries[0].linksComplete {
				t.Fatal("conflicting/truncated links certified")
			}
		})
	}
}

func TestStudyLinksRejectCrossTradeOrder(t *testing.T) {
	base, _ := entriesFixture(t)
	base.tx.rows.closed = true
	a, b := uuid.New(), uuid.New()
	tx := &studyTx{entryTestTx: base.tx, links: []linkWire{{a, "300", "exit"}, {b, "300", "exit"}}}
	batch := EntryBatch{entries: []entryRecord{{rowID: a, orderID: "100"}, {rowID: b, orderID: "200"}}}
	if err := loadEntryLinks(context.Background(), tx, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.entries[0].linksComplete || batch.entries[1].linksComplete {
		t.Fatal("reused order attributed to two trades")
	}
}
