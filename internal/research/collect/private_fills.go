//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"time"
)

type FillWindow struct {
	Symbol   string
	From, To time.Time
	MaxPages int
}

// FillCoverage concerns API record timestamps only. It neither proves coverage
// of a position's execution-time history nor sets LedgerInput.ArchiveComplete.
type FillCoverage struct {
	Status                 string `json:"status"`
	Reason                 string `json:"reason"`
	Pages                  int    `json:"pages"`
	Fills                  int    `json:"fills"`
	ExecutionOutsideWindow int    `json:"execution_outside_record_window"`
	RecordWindowComplete   bool   `json:"record_window_complete"`
}
type FillArchive struct {
	Coverage FillCoverage
	fills    []privateFill
}
type privateFill struct {
	Fill       Fill
	RecordedAt time.Time
}

func (FillArchive) MarshalJSON() ([]byte, error) { return nil, errPrivateEvidence }
func (privateFill) MarshalJSON() ([]byte, error) { return nil, errPrivateEvidence }
func (FillArchive) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "[private fill archive]") }
func (privateFill) Format(s fmt.State, _ rune)   { _, _ = io.WriteString(s, "[private fill]") }

var feeCurrency = regexp.MustCompile(`^[A-Z0-9]{1,16}$`)

// CollectFills rereads a bounded record window on every call. Private bill IDs
// exist only during this pass; no cursor/raw response is persisted. Caller must
// reconcile explicit orders, time coverage and funding separately.
func (c *PrivateClient) CollectFills(ctx context.Context, w FillWindow) (FillArchive, error) {
	a := FillArchive{Coverage: FillCoverage{Status: "pending"}, fills: []privateFill{}}
	fail := func(reason string) (FillArchive, error) { a.Coverage.Reason = reason; return a, errors.New(reason) }
	if c == nil || c.engine == nil || !c.credentials.valid() {
		return fail("research_private_client_unavailable")
	}
	now := c.engine.now()
	if !usdtSwap.MatchString(w.Symbol) || w.From.IsZero() || !w.To.After(w.From) || w.From.Nanosecond()%int(time.Millisecond) != 0 || w.To.Nanosecond()%int(time.Millisecond) != 0 || w.To.Sub(w.From) > 31*24*time.Hour || w.From.Before(now.Add(-80*24*time.Hour)) || w.To.After(now) || w.From.UnixMilli() < 1 || w.MaxPages < 1 || w.MaxPages > 200 {
		return fail("research_fill_window_invalid")
	}
	if ctx.Err() != nil {
		return fail("archive_cancelled")
	}
	if err := c.verifyAccount(ctx); err != nil {
		return fail(err.Error())
	}
	a.Coverage.Status = "incomplete"
	q := url.Values{"instType": {"SWAP"}, "instId": {w.Symbol}, "limit": {"100"}, "begin": {strconv.FormatInt(w.From.UnixMilli()-1, 10)}, "end": {strconv.FormatInt(w.To.UnixMilli(), 10)}}
	seen := map[string]privateFill{}
	cursor := ""
	for page := 0; page < w.MaxPages; page++ {
		if ctx.Err() != nil {
			return fail("archive_cancelled")
		}
		if cursor != "" {
			q.Set("after", cursor)
		}
		body, err := c.getFillPage(ctx, q)
		if err != nil {
			return fail(err.Error())
		}
		a.Coverage.Pages++
		rows, err := decodePrivateFills(body, w, now)
		if err != nil {
			return fail("research_fill_page_invalid")
		}
		// Cancellation may race a successful final response. Do not certify
		// exhaustion after the caller has already cancelled the pass.
		if ctx.Err() != nil {
			return fail("archive_cancelled")
		}
		if len(rows) == 0 {
			a.Coverage.Status = "complete"
			a.Coverage.RecordWindowComplete = true
			sort.Slice(a.fills, func(i, j int) bool {
				if !a.fills[i].Fill.At.Equal(a.fills[j].Fill.At) {
					return a.fills[i].Fill.At.Before(a.fills[j].Fill.At)
				}
				return comparePrivateID(a.fills[i].Fill.ID, a.fills[j].Fill.ID) < 0
			})
			return a, nil
		}
		next := ""
		// Validate a whole page before accepting it; an ambiguous page must not
		// contribute partially accepted rows or mark any coverage complete.
		fresh := map[string]privateFill{}
		for _, r := range rows {
			id := r.Fill.ID
			if next == "" || comparePrivateID(id, next) < 0 {
				next = id
			}
			prior, exists := seen[id]
			if !exists {
				prior, exists = fresh[id]
			}
			if exists {
				if !sameFill(prior.Fill, r.Fill) || !prior.RecordedAt.Equal(r.RecordedAt) {
					return fail("research_fill_duplicate_conflict")
				}
				continue
			}
			if cursor != "" && comparePrivateID(id, cursor) >= 0 {
				return fail("research_fill_cursor_invalid")
			}
			fresh[id] = r
		}
		if len(fresh) == 0 || cursor != "" && comparePrivateID(next, cursor) >= 0 {
			return fail("research_fill_no_progress")
		}
		for id, r := range fresh {
			seen[id] = r
			if r.RecordedAt.Before(w.From) || !r.RecordedAt.Before(w.To) {
				continue
			}
			a.fills = append(a.fills, r)
			a.Coverage.Fills++
			if r.Fill.At.Before(w.From) || !r.Fill.At.Before(w.To) {
				a.Coverage.ExecutionOutsideWindow++
			}
		}
		cursor = next
	}
	return fail("page_budget_exhausted")
}

func comparePrivateID(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func privateTime(raw string) (time.Time, bool) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > 253402300799999 || strconv.FormatInt(n, 10) != raw {
		return time.Time{}, false
	}
	return time.UnixMilli(n).UTC(), true
}
func decodePrivateFills(body []byte, w FillWindow, now time.Time) ([]privateFill, error) {
	if !exactPrivateResponse(body, []string{"billId", "ordId", "instType", "instId", "side", "posSide", "fillSz", "fillPx", "fillPnl", "fee", "feeCcy", "ts", "fillTime"}) {
		return nil, errors.New("research_fill_page_invalid")
	}
	var envelope struct {
		Data []struct {
			Bill        string `json:"billId"`
			Order       string `json:"ordId"`
			Type        string `json:"instType"`
			Symbol      string `json:"instId"`
			Side        string `json:"side"`
			PosSide     string `json:"posSide"`
			Size        string `json:"fillSz"`
			Price       string `json:"fillPx"`
			PnL         string `json:"fillPnl"`
			Fee         string `json:"fee"`
			FeeCurrency string `json:"feeCcy"`
			Recorded    string `json:"ts"`
			Executed    string `json:"fillTime"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Data) > 100 {
		return nil, errors.New("research_fill_page_invalid")
	}
	result := make([]privateFill, 0, len(envelope.Data))
	for _, row := range envelope.Data {
		at, aok := privateTime(row.Executed)
		recorded, rok := privateTime(row.Recorded)
		size, sok := decimal(row.Size)
		price, pok := decimal(row.Price)
		_, fok := decimal(row.Fee)
		_, nok := decimal(row.PnL)
		if !privateID.MatchString(row.Bill) || !privateID.MatchString(row.Order) || row.Type != "SWAP" || row.Symbol != w.Symbol || (row.Side != "buy" && row.Side != "sell") || (row.PosSide != "long" && row.PosSide != "short" && row.PosSide != "net") || !feeCurrency.MatchString(row.FeeCurrency) || !aok || !rok || at.After(now) || recorded.Before(w.From.Add(-time.Millisecond)) || recorded.After(w.To) || !sok || size.Sign() <= 0 || !pok || price.Sign() <= 0 || !fok || !nok {
			return nil, errors.New("research_fill_page_invalid")
		}
		result = append(result, privateFill{Fill{ID: row.Bill, OrderID: row.Order, Symbol: row.Symbol, Side: row.Side, PosSide: row.PosSide, At: at, Size: row.Size, Price: row.Price, Fee: row.Fee, PnL: row.PnL, FeeCurrency: row.FeeCurrency}, recorded})
	}
	return result, nil
}
