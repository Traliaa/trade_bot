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

type MarketWindow struct {
	Symbol   string
	From, To time.Time
	MaxPages int
}

// WindowTraversed only describes the observed API traversal. Sparse sources
// need independent schedule/attribution evidence before claiming completeness.
type ArchiveCoverage struct {
	Status          string `json:"status"`
	Reason          string `json:"reason"`
	Pages           int    `json:"pages"`
	Records         int    `json:"records"`
	WindowTraversed bool   `json:"window_traversed"`
}

func validateMarketWindow(c *ArchiveClient, w MarketWindow, minute bool) (time.Time, error) {
	if c == nil || c.now == nil {
		return time.Time{}, errors.New("archive_client_unavailable")
	}
	now := c.now()
	if !usdtSwap.MatchString(w.Symbol) || w.From.IsZero() || w.From.UnixMilli() < 1 || w.From.Year() > 9999 || w.To.Year() > 9999 || !w.To.After(w.From) || w.To.Sub(w.From) > 72*time.Hour || w.From.Before(now.Add(-80*24*time.Hour)) || w.To.After(now) || w.MaxPages < 1 || w.MaxPages > 200 || w.From.Nanosecond()%int(time.Millisecond) != 0 || w.To.Nanosecond()%int(time.Millisecond) != 0 {
		return time.Time{}, errors.New("market_window_invalid")
	}
	if minute && (!w.From.Equal(w.From.Truncate(time.Minute)) || !w.To.Equal(w.To.Truncate(time.Minute))) {
		return time.Time{}, errors.New("market_minute_bounds_required")
	}
	return now, nil
}

func marketRows(body []byte) ([]json.RawMessage, error) {
	if !exactPrivateObject(body, []string{"code", "data"}, true) {
		return nil, errors.New("market_page_invalid")
	}
	var e struct {
		Code string            `json:"code"`
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &e) != nil || e.Code != "0" || e.Data == nil || len(e.Data) > 100 {
		return nil, errors.New("market_page_invalid")
	}
	return e.Data, nil
}

// walkMarket is intentionally internal: endpoint/query/row contracts come from
// the typed adapters, never from arbitrary caller URLs. It does not persist rows.
func walkMarket[T any](ctx context.Context, c *ArchiveClient, w MarketWindow, path string, q url.Values, cursor int64, decode func([]byte) ([]T, error), at func(T) time.Time, equal func(T, T) bool) ([]T, ArchiveCoverage, error) {
	coverage := ArchiveCoverage{Status: "pending"}
	selected := []T{}
	finish := func(reason string) ([]T, ArchiveCoverage, error) {
		sort.Slice(selected, func(i, j int) bool { return at(selected[i]).Before(at(selected[j])) })
		coverage.Records = len(selected)
		coverage.Reason = reason
		if reason != "" {
			return selected, coverage, errors.New(reason)
		}
		coverage.Status = "complete"
		coverage.WindowTraversed = true
		return selected, coverage, nil
	}
	if ctx.Err() != nil {
		return finish("archive_cancelled")
	}
	coverage.Status = "incomplete"
	seen := map[int64]T{}
	q.Set("instId", w.Symbol)
	q.Set("limit", "100")
	for page := 0; page < w.MaxPages; page++ {
		if ctx.Err() != nil {
			return finish("archive_cancelled")
		}
		q.Set("after", strconv.FormatInt(cursor, 10))
		body, err := c.Get(ctx, path, q)
		if err != nil {
			return finish(err.Error())
		}
		coverage.Pages++
		rows, err := decode(body)
		if err != nil {
			return finish("market_page_invalid")
		}
		if ctx.Err() != nil {
			return finish("archive_cancelled")
		}
		if len(rows) == 0 {
			return finish("")
		}
		fresh := map[int64]T{}
		oldest := int64(1<<63 - 1)
		for _, row := range rows {
			ts := at(row).UnixMilli()
			if ts < oldest {
				oldest = ts
			}
			prior, exists := seen[ts]
			if !exists {
				prior, exists = fresh[ts]
			}
			if exists {
				if !equal(prior, row) {
					return finish("market_duplicate_conflict")
				}
				continue
			}
			if ts > cursor {
				return finish("market_cursor_invalid")
			}
			fresh[ts] = row
		}
		if len(fresh) == 0 || oldest >= cursor {
			return finish("market_no_progress")
		}
		for ts, row := range fresh {
			seen[ts] = row
			if ts >= w.From.UnixMilli() && ts < w.To.UnixMilli() {
				selected = append(selected, row)
			}
		}
		if oldest < w.From.UnixMilli() {
			return finish("")
		}
		cursor = oldest
	}
	return finish("page_budget_exhausted")
}
