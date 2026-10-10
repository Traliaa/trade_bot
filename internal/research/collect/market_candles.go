//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

type Candle struct {
	At             time.Time `json:"at"`
	Prices         OHLC      `json:"prices"`
	Volume         *string   `json:"volume_contracts"`
	VolumeCurrency *string   `json:"volume_currency"`
	VolumeQuote    *string   `json:"volume_quote"`
}
type CandleArchive struct {
	Symbol          string          `json:"symbol"`
	Source          string          `json:"source"`
	From            time.Time       `json:"from"`
	To              time.Time       `json:"to"`
	Coverage        ArchiveCoverage `json:"coverage"`
	ExpectedMinutes int             `json:"expected_minutes"`
	MissingMinutes  int             `json:"missing_minutes"`
	Candles         []Candle        `json:"candles"`
}

// CollectCandles accepts only full-minute intervals. Non-minute boundary
// segments need public trade evidence; mark candles are not settlement prices.
func (c *ArchiveClient) CollectCandles(ctx context.Context, w MarketWindow, source string) (CandleArchive, error) {
	a := CandleArchive{Coverage: ArchiveCoverage{Status: "pending"}, Candles: []Candle{}}
	path := "/api/v5/market/history-candles"
	if source != "trade" && source != "mark" {
		a.Coverage.Reason = "market_source_invalid"
		return a, errors.New(a.Coverage.Reason)
	}
	if source == "mark" {
		path = "/api/v5/market/history-mark-price-candles"
	}
	now, err := validateMarketWindow(c, w, true)
	if err != nil {
		a.Coverage.Reason = err.Error()
		return a, err
	}
	a.Symbol = w.Symbol
	a.Source = source
	a.From = w.From.UTC()
	a.To = w.To.UTC()
	a.ExpectedMinutes = int(w.To.Sub(w.From) / time.Minute)
	// Candle timestamps lie on a minute grid. Asking just below To excludes
	// its still-open candle with either inclusive or exclusive API cursors,
	// without losing any requested full minute.
	a.Candles, a.Coverage, err = walkMarket(ctx, c, w, path, url.Values{"bar": {"1m"}}, w.To.UnixMilli()-1, func(body []byte) ([]Candle, error) { return decodeCandles(body, source, now) }, func(row Candle) time.Time { return row.At }, sameCandle)
	a.MissingMinutes = a.ExpectedMinutes - len(a.Candles)
	if err == nil && a.MissingMinutes != 0 {
		a.Coverage.Status = "incomplete"
		a.Coverage.Reason = "market_minute_gaps"
		err = errors.New(a.Coverage.Reason)
	}
	return a, err
}

func decodeCandles(body []byte, source string, now time.Time) ([]Candle, error) {
	rows, err := marketRows(body)
	if err != nil {
		return nil, err
	}
	result := make([]Candle, 0, len(rows))
	for _, raw := range rows {
		var r []string
		n := 9
		if source == "mark" {
			n = 6
		}
		if json.Unmarshal(raw, &r) != nil || len(r) != n || r[n-1] != "1" {
			return nil, errors.New("market_candle_invalid")
		}
		at, ok := privateTime(r[0])
		if !ok || !at.Equal(at.Truncate(time.Minute)) || at.Add(time.Minute).After(now) {
			return nil, errors.New("market_candle_invalid")
		}
		o, ook := decimal(r[1])
		h, hok := decimal(r[2])
		l, lok := decimal(r[3])
		close, cok := decimal(r[4])
		if !ook || !hok || !lok || !cok || o.Sign() <= 0 || h.Sign() <= 0 || l.Sign() <= 0 || close.Sign() <= 0 || l.Cmp(o) > 0 || l.Cmp(close) > 0 || h.Cmp(o) < 0 || h.Cmp(close) < 0 {
			return nil, errors.New("market_candle_invalid")
		}
		row := Candle{At: at, Prices: OHLC{r[1], r[2], r[3], r[4]}}
		if source == "trade" {
			for _, v := range r[5:8] {
				d, ok := decimal(v)
				if !ok || d.Sign() < 0 {
					return nil, errors.New("market_candle_invalid")
				}
			}
			row.Volume = &r[5]
			row.VolumeCurrency = &r[6]
			row.VolumeQuote = &r[7]
		}
		result = append(result, row)
	}
	return result, nil
}
func sameCandle(a, b Candle) bool {
	return a.At.Equal(b.At) && sameDecimal(a.Prices.Open, b.Prices.Open) && sameDecimal(a.Prices.High, b.Prices.High) && sameDecimal(a.Prices.Low, b.Prices.Low) && sameDecimal(a.Prices.Close, b.Prices.Close) && sameVolume(a.Volume, b.Volume) && sameVolume(a.VolumeCurrency, b.VolumeCurrency) && sameVolume(a.VolumeQuote, b.VolumeQuote)
}
func sameVolume(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return sameDecimal(*a, *b)
}
