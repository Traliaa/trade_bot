package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/universe"
)

// GetCandles returns up to limit closed candles, oldest first. OKX's limit
// includes the current unconfirmed candle, so request a spare row and paginate
// when filtering or the exchange's 300-row page limit leaves us short.
func (s *Service) GetCandles(ctx context.Context, instID, bar string, limit int) ([]models.CandleTick, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	if limit > 3000 {
		return nil, fmt.Errorf("closed candle limit %d exceeds 3000", limit)
	}
	out := make([]models.CandleTick, 0, limit)
	seen := make(map[int64]bool, limit)
	after := ""
	for page := 0; page < 12 && len(out) < limit; page++ {
		requestLimit := min(limit-len(out)+1, 300)
		candles, next, err := s.getCandlePage(ctx, instID, bar, requestLimit, after)
		if err != nil {
			return nil, err
		}
		for _, candle := range candles {
			key := candle.Start.UnixMilli()
			if !seen[key] {
				seen[key] = true
				out = append(out, candle)
			}
		}
		if next == "" || next == after {
			break
		}
		after = next
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// CandleRow: OKX data row: [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm]
func (s *Service) getCandlePage(ctx context.Context, instID, bar string, limit int, after string) ([]models.CandleTick, string, error) {
	select {
	case <-ctx.Done():
		return nil, "", ctx.Err()
	case <-time.After(time.Second):
	}
	bar, err := okxBar(bar) // cfg.HTF = "1h" -> "1H"
	if err != nil {
		return nil, "", err
	}

	u := fmt.Sprintf("https://www.okx.com/api/v5/market/candles?instId=%s&bar=%s&limit=%d",
		url.QueryEscape(instID), url.QueryEscape(bar), limit,
	)
	if after != "" {
		u += "&after=" + url.QueryEscape(after)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode/100 != 2 {
		return nil, "", fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	var r struct {
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, "", err
	}
	if r.Code != "0" {
		return nil, "", fmt.Errorf("okx candles error: code=%s msg=%s", r.Code, r.Msg)
	}

	tfDur := timeframeToDuration(bar)

	// OKX обычно отдаёт newest-first → разворачиваем, чтобы прогрев шёл по времени
	out := make([]models.CandleTick, 0, len(r.Data))
	var oldest int64
	before, _ := strconv.ParseInt(after, 10, 64)
	for i := len(r.Data) - 1; i >= 0; i-- {
		row := r.Data[i]
		if len(row) == 0 {
			continue
		}

		tsMs, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil || tsMs <= 0 || (before > 0 && tsMs >= before) {
			continue
		}
		if oldest == 0 || tsMs < oldest {
			oldest = tsMs
		}
		if len(row) < 9 || row[8] != "1" {
			continue
		}
		open, _ := strconv.ParseFloat(row[1], 64)
		high, _ := strconv.ParseFloat(row[2], 64)
		low, _ := strconv.ParseFloat(row[3], 64)
		closep, _ := strconv.ParseFloat(row[4], 64)
		if !universe.FinitePositive(open) || !universe.FinitePositive(high) || !universe.FinitePositive(low) || !universe.FinitePositive(closep) || high < open || high < closep || low > open || low > closep {
			continue
		}

		start := time.UnixMilli(tsMs)
		end := start.Add(tfDur)
		if end.After(time.Now()) {
			continue
		}

		var vol float64
		if len(row) >= 6 {
			vol, _ = strconv.ParseFloat(row[5], 64)
		}
		var volQuote float64
		if len(row) >= 8 {
			volQuote, _ = strconv.ParseFloat(row[7], 64)
		}

		out = append(out, models.CandleTick{
			InstID:       instID,
			Open:         open,
			High:         high,
			Low:          low,
			Close:        closep,
			Volume:       vol,
			QuoteVolume:  volQuote,
			Start:        start,
			End:          end,
			TimeframeRaw: bar,
		})
	}

	next := ""
	if oldest > 0 {
		next = strconv.FormatInt(oldest, 10)
	}
	return out, next, nil
}
