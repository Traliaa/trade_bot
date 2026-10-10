//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

type FundingRate struct {
	At           time.Time `json:"at"`
	RealizedRate string    `json:"realized_rate"`
}
type FundingArchive struct {
	Symbol           string          `json:"symbol"`
	Source           string          `json:"source"`
	From             time.Time       `json:"from"`
	To               time.Time       `json:"to"`
	Coverage         ArchiveCoverage `json:"coverage"`
	ScheduleVerified bool            `json:"schedule_verified"`
	Costs            string          `json:"costs"`
	Rates            []FundingRate   `json:"rates"`
}

// CollectFunding observes public realized rates, not account funding charges.
// Even an exhausted API window cannot establish a complete settlement schedule.
// No missing rate, empty window or mark approximation can certify zero costs.
func (c *ArchiveClient) CollectFunding(ctx context.Context, w MarketWindow) (FundingArchive, error) {
	a := FundingArchive{Coverage: ArchiveCoverage{Status: "pending"}, Costs: "unknown", Rates: []FundingRate{}}
	now, err := validateMarketWindow(c, w, false)
	if err != nil {
		a.Coverage.Reason = err.Error()
		return a, err
	}
	a.Symbol = w.Symbol
	a.Source = "okx_realized_funding_rate"
	a.From = w.From.UTC()
	a.To = w.To.UTC()
	a.Rates, a.Coverage, err = walkMarket(ctx, c, w, "/api/v5/public/funding-rate-history", url.Values{}, w.To.UnixMilli()+1, func(body []byte) ([]FundingRate, error) { return decodeFunding(body, w.Symbol, now) }, func(row FundingRate) time.Time { return row.At }, func(a, b FundingRate) bool { return a.At.Equal(b.At) && sameDecimal(a.RealizedRate, b.RealizedRate) })
	if len(a.Rates) > 0 {
		a.Costs = "sensitivity-only"
	}
	return a, err
}

func decodeFunding(body []byte, symbol string, now time.Time) ([]FundingRate, error) {
	rows, err := marketRows(body)
	if err != nil {
		return nil, err
	}
	result := make([]FundingRate, 0, len(rows))
	for _, raw := range rows {
		if !exactPrivateObject(raw, []string{"instId", "instType", "fundingTime", "realizedRate"}, true) {
			return nil, errors.New("market_funding_invalid")
		}
		var row struct {
			Symbol string `json:"instId"`
			Type   string `json:"instType"`
			Time   string `json:"fundingTime"`
			Rate   string `json:"realizedRate"`
		}
		if json.Unmarshal(raw, &row) != nil || row.Symbol != symbol || row.Type != "SWAP" {
			return nil, errors.New("market_funding_invalid")
		}
		at, ok := privateTime(row.Time)
		_, valid := decimal(row.Rate)
		if !ok || !valid || at.After(now) {
			return nil, errors.New("market_funding_invalid")
		}
		result = append(result, FundingRate{At: at, RealizedRate: row.Rate})
	}
	return result, nil
}
