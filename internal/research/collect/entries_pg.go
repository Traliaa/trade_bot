package collect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"trade_bot/internal/models"
)

type EntryWindow struct {
	From, To time.Time
	Protocol string
	Limit    int
}

// EntryCoverage describes only the bounded DB batch, never exchange/archive
// completeness. When Truncated, split the window or explicitly report incomplete;
// do not persist row IDs as a cursor or silently drop tied timestamps.
type EntryCoverage struct {
	Rows               int  `json:"rows"`
	Complete           int  `json:"capture_complete"`
	Incomplete         int  `json:"capture_incomplete"`
	Missing            int  `json:"capture_missing"`
	Invalid            int  `json:"capture_invalid"`
	AttributionPending int  `json:"attribution_pending"`
	Truncated          bool `json:"truncated"`
}

type EntryBatch struct {
	Coverage EntryCoverage
	entries  []entryRecord
}
type entryRecord struct {
	rowID                  uuid.UUID
	orderID, symbol, state string
	entryAt                time.Time
	capture                *models.ResearchCaptureInput
	captureStatus          string
	exitOrders             []string
	linksComplete          bool
}

var errPrivateEvidence = errors.New("private_evidence_not_serializable")

func (EntryBatch) MarshalJSON() ([]byte, error)  { return nil, errPrivateEvidence }
func (entryRecord) MarshalJSON() ([]byte, error) { return nil, errPrivateEvidence }
func (EntryBatch) Format(s fmt.State, _ rune)    { _, _ = io.WriteString(s, "[private entry batch]") }
func (entryRecord) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "[private entry record]") }

var entryProtocol = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var errDB = errors.New("research_db_unavailable")
var errDBRows = errors.New("research_db_rows_invalid")

// Only a separately provisioned, account-scoped view is queried. SQL-side bounds
// prevent malformed values from allocating arbitrary client memory. Full mutable
// payload, user/account fields and credentials are not selected, even transiently.
const entrySelect = `SELECT row_id,
 COALESCE(CASE WHEN octet_length(inst_id) <= 40 THEN inst_id END, ''),
 COALESCE(CASE WHEN octet_length(status) <= 16 THEN status END, ''),
 entry_at,
 COALESCE(CASE WHEN octet_length(open_order_id) <= 128 THEN open_order_id END, ''),
 research_entry_snapshot IS NOT NULL,
 CASE WHEN octet_length(research_entry_snapshot::text) <= 32768 THEN research_entry_snapshot::text END
 FROM research_collect.entries_v1
 WHERE entry_at >= $1 AND entry_at < $2
 ORDER BY entry_at, row_id
 LIMIT $3`

type entryDB interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// ReadEntries owns one short transaction but not the supplied pool. The caller
// must use the dedicated SELECT-only role/view, never the trading service pool.
// This adapter never discovers credentials or opens a separate network source.
func ReadEntries(ctx context.Context, pool *pgxpool.Pool, window EntryWindow) (EntryBatch, error) {
	if pool == nil {
		return EntryBatch{}, errDB
	}
	return readEntries(ctx, pool, window)
}

func readEntries(ctx context.Context, db entryDB, window EntryWindow) (EntryBatch, error) {
	return readEntryBatch(ctx, db, window, false)
}

func ReadStudyEntries(ctx context.Context, pool *pgxpool.Pool, window EntryWindow) (EntryBatch, error) {
	if pool == nil {
		return EntryBatch{}, errDB
	}
	return readStudyEntries(ctx, pool, window)
}
func readStudyEntries(ctx context.Context, db entryDB, window EntryWindow) (EntryBatch, error) {
	return readEntryBatch(ctx, db, window, true)
}
func readEntryBatch(ctx context.Context, db entryDB, window EntryWindow, links bool) (EntryBatch, error) {
	// pgx/PostgreSQL truncate timestamp parameters to microseconds. Refuse a
	// window that cannot be represented exactly instead of losing boundary rows.
	if window.From.IsZero() || window.To.IsZero() || window.From.Year() < 1970 || window.To.Year() > 9999 || window.From.Nanosecond()%1000 != 0 || window.To.Nanosecond()%1000 != 0 || !window.To.After(window.From) || window.To.Sub(window.From) > 28*24*time.Hour || window.Limit < 1 || window.Limit > 1000 || !entryProtocol.MatchString(window.Protocol) {
		return EntryBatch{}, errors.New("research_db_window_invalid")
	}
	if ctx.Err() != nil {
		return EntryBatch{}, errDB
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return EntryBatch{}, errDB
	}
	defer func() {
		// Cancellation of the read must not cancel cleanup. pgx closes a broken
		// connection if rollback fails; the pool must never get an open tx back.
		cleanup, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout = '5s'; SET LOCAL lock_timeout = '1s'"); err != nil {
		return EntryBatch{}, errDB
	}
	rows, err := tx.Query(ctx, entrySelect, window.From.UTC(), window.To.UTC(), window.Limit+1)
	if err != nil {
		return EntryBatch{}, errDB
	}
	defer rows.Close()
	b := EntryBatch{entries: make([]entryRecord, 0, window.Limit)}
	ids := map[uuid.UUID]bool{}
	captures := map[string]int{}
	for rows.Next() {
		if ctx.Err() != nil {
			return EntryBatch{}, errDB
		}
		if len(b.entries) == window.Limit {
			b.Coverage.Truncated = true
			break
		}
		var e entryRecord
		var present bool
		var raw *string
		if err := rows.Scan(&e.rowID, &e.symbol, &e.state, &e.entryAt, &e.orderID, &present, &raw); err != nil {
			return EntryBatch{}, errDB
		}
		if e.rowID == uuid.Nil || ids[e.rowID] || e.entryAt.Before(window.From) || !e.entryAt.Before(window.To) || len(e.symbol) > 40 || len(e.state) > 16 || len(e.orderID) > 128 {
			return EntryBatch{}, errDBRows
		}
		if n := len(b.entries); n > 0 {
			prev := b.entries[n-1]
			if e.entryAt.Before(prev.entryAt) || e.entryAt.Equal(prev.entryAt) && e.rowID.String() <= prev.rowID.String() {
				return EntryBatch{}, errDBRows
			}
		}
		ids[e.rowID] = true
		e.captureStatus = "invalid"
		if !present && raw == nil {
			e.captureStatus = "missing"
		} else if present && raw != nil {
			in, status, err := DecodeSnapshot([]byte(*raw), window.Protocol)
			// PostgreSQL timestamps have microsecond precision, while the snapshot
			// retains the producer's original nanoseconds. Do not reject that loss.
			if err == nil && in.Symbol == e.symbol && in.EntryAt.Truncate(time.Microsecond).Equal(e.entryAt) && (e.state == "open" || e.state == "closed") {
				e.capture = &in
				e.captureStatus = status
				if prev, exists := captures[in.CaptureID]; exists {
					b.entries[prev].captureStatus = "invalid"
					b.entries[prev].capture = nil
					e.captureStatus = "invalid"
					e.capture = nil
				} else {
					captures[in.CaptureID] = len(b.entries)
				}
			}
		}
		b.entries = append(b.entries, e)
	}
	if rows.Err() != nil || ctx.Err() != nil {
		return EntryBatch{}, errDB
	}
	rows.Close()
	if links {
		if err := loadEntryLinks(ctx, tx, &b); err != nil {
			return EntryBatch{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return EntryBatch{}, errDB
	}
	for _, e := range b.entries {
		b.Coverage.Rows++
		switch e.captureStatus {
		case "complete":
			b.Coverage.Complete++
		case "incomplete":
			b.Coverage.Incomplete++
		case "missing":
			b.Coverage.Missing++
		default:
			b.Coverage.Invalid++
		}
		if e.orderID == "" {
			b.Coverage.AttributionPending++
		}
	}
	return b, nil
}
