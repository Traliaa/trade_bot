//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// PublicTrade IDs identify public market events, not account fills. Preserve
// decimal source strings; no float conversion or private identifiers belong here.
type PublicTrade struct {
	ID     string    `json:"trade_id"`
	At     time.Time `json:"at"`
	Price  string    `json:"price"`
	Size   string    `json:"size"`
	Side   string    `json:"side"`
	Source string    `json:"source"`
}

type BoundaryArchive struct {
	Symbol   string          `json:"symbol"`
	Source   string          `json:"source"`
	Segment  Segment         `json:"segment"`
	Coverage ArchiveCoverage `json:"coverage"`
	Report   BoundaryReport  `json:"report"`
	Trades   []PublicTrade   `json:"trades"`
}

// CollectBoundaryTrades reads a single partial minute. Timestamp paging is used
// only to seek the upper edge. Subsequent ID paging preserves saturated time
// ties. WindowTraversed describes the observed API, not an independently proven
// immutable exchange snapshot or the ordering of events within a millisecond.
func (c *ArchiveClient) CollectBoundaryTrades(ctx context.Context, w MarketWindow) (BoundaryArchive, error) {
	a := BoundaryArchive{Coverage: ArchiveCoverage{Status: "pending"}, Report: BoundaryReport{Coverage: "incomplete", Reasons: []string{"source_incomplete"}}, Trades: []PublicTrade{}}
	now, err := validateMarketWindow(c, w, false)
	if err == nil && (w.To.Sub(w.From) >= time.Minute || w.To.After(w.From.Truncate(time.Minute).Add(time.Minute))) {
		err = errors.New("market_boundary_bounds_required")
	}
	if err != nil {
		a.Coverage.Reason = err.Error()
		return a, err
	}
	a.Symbol, a.Source = w.Symbol, "okx_public_trades"
	a.Segment = Segment{Kind: "boundary", Start: w.From.UTC(), End: w.To.UTC()}
	finish := func(reason string) (BoundaryArchive, error) {
		sort.Slice(a.Trades, func(i, j int) bool {
			if !a.Trades[i].At.Equal(a.Trades[j].At) {
				return a.Trades[i].At.Before(a.Trades[j].At)
			}
			return comparePrivateID(a.Trades[i].ID, a.Trades[j].ID) < 0
		})
		a.Coverage.Records = len(a.Trades)
		ticks := make([]Tick, 0, len(a.Trades))
		for _, row := range a.Trades {
			ticks = append(ticks, Tick{ID: row.ID, At: row.At, Price: row.Price})
		}
		var reportErr error
		a.Report, reportErr = BoundaryOHLC(a.Segment, ticks, a.Coverage.WindowTraversed)
		if reportErr != nil {
			reason = "market_boundary_invalid"
		} else if reason == "" && a.Report.Coverage != "complete" {
			reason = "market_tick_order_ambiguous"
		}
		a.Coverage.Reason = reason
		if reason != "" {
			return a, errors.New(reason)
		}
		a.Coverage.Status = "complete"
		return a, nil
	}
	if ctx.Err() != nil {
		return finish("archive_cancelled")
	}
	a.Coverage.Status = "incomplete"
	seen := map[string]PublicTrade{}
	q := url.Values{"instId": {w.Symbol}, "limit": {"100"}, "type": {"2"}, "after": {strconv.FormatInt(w.To.UnixMilli(), 10)}}
	cursor := ""
	oldestTime := w.To
	for page := 0; page < w.MaxPages; page++ {
		if ctx.Err() != nil {
			return finish("archive_cancelled")
		}
		body, err := c.Get(ctx, "/api/v5/market/history-trades", q)
		if err != nil {
			return finish(err.Error())
		}
		a.Coverage.Pages++
		rows, err := decodePublicTrades(body, w.Symbol, now)
		if err != nil {
			return finish("market_trade_page_invalid")
		}
		if ctx.Err() != nil {
			return finish("archive_cancelled")
		}
		if len(rows) == 0 {
			a.Coverage.WindowTraversed = true
			return finish("")
		}
		// Response order is not evidence: sort by numeric ID, then validate time
		// monotonicity before relying on the lower time edge to stop traversal.
		sort.Slice(rows, func(i, j int) bool { return comparePrivateID(rows[i].ID, rows[j].ID) > 0 })
		fresh := map[string]PublicTrade{}
		nextCursor, nextTime := cursor, oldestTime
		for _, row := range rows {
			prior, exists := seen[row.ID]
			if !exists {
				prior, exists = fresh[row.ID]
			}
			if exists {
				if !samePublicTrade(prior, row) {
					return finish("market_duplicate_conflict")
				}
				continue
			}
			if cursor != "" && comparePrivateID(row.ID, cursor) >= 0 {
				return finish("market_cursor_invalid")
			}
			if row.At.After(nextTime) {
				return finish("market_trade_time_order_invalid")
			}
			fresh[row.ID] = row
			nextCursor, nextTime = row.ID, row.At
		}
		if len(fresh) == 0 {
			return finish("market_no_progress")
		}
		// Accept a page atomically, including validation of out-of-window rows.
		for id, row := range fresh {
			seen[id] = row
			if !row.At.Before(w.From) && row.At.Before(w.To) {
				a.Trades = append(a.Trades, row)
			}
		}
		if nextTime.Before(w.From) {
			a.Coverage.WindowTraversed = true
			return finish("")
		}
		cursor, oldestTime = nextCursor, nextTime
		q.Set("type", "1")
		q.Set("after", cursor)
	}
	return finish("page_budget_exhausted")
}

func decodePublicTrades(body []byte, symbol string, now time.Time) ([]PublicTrade, error) {
	rows, err := marketRows(body)
	if err != nil {
		return nil, err
	}
	result := make([]PublicTrade, 0, len(rows))
	for _, raw := range rows {
		if !exactPrivateObject(raw, []string{"instId", "tradeId", "ts", "px", "sz", "side", "source"}, true) {
			return nil, errors.New("market_trade_invalid")
		}
		var row struct {
			Symbol string `json:"instId"`
			ID     string `json:"tradeId"`
			Time   string `json:"ts"`
			Price  string `json:"px"`
			Size   string `json:"sz"`
			Side   string `json:"side"`
			Source string `json:"source"`
		}
		if json.Unmarshal(raw, &row) != nil || row.Symbol != symbol || len(row.ID) > 64 || !privateID.MatchString(row.ID) || (row.Side != "buy" && row.Side != "sell") || (row.Source != "0" && row.Source != "1") {
			return nil, errors.New("market_trade_invalid")
		}
		at, validTime := privateTime(row.Time)
		price, validPrice := decimal(row.Price)
		size, validSize := decimal(row.Size)
		if !validTime || at.After(now) || !validPrice || price.Sign() <= 0 || !validSize || size.Sign() <= 0 {
			return nil, errors.New("market_trade_invalid")
		}
		result = append(result, PublicTrade{ID: row.ID, At: at, Price: row.Price, Size: row.Size, Side: row.Side, Source: row.Source})
	}
	return result, nil
}

func samePublicTrade(a, b PublicTrade) bool {
	return a.ID == b.ID && a.At.Equal(b.At) && sameDecimal(a.Price, b.Price) && sameDecimal(a.Size, b.Size) && a.Side == b.Side && a.Source == b.Source
}
