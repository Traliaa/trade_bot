package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"trade_bot/internal/models"
)

// CandleRow: OKX data row: [ts, o, h, l, c, vol, volCcy, volCcyQuote, confirm]
func (c *Client) GetCandles(ctx context.Context, instID, bar string, limit int) ([]models.CandleTick, error) {
	if limit <= 0 {
		limit = 100
	}
	time.Sleep(1000 * time.Millisecond)
	bar, err := okxBar(bar) // cfg.HTF = "1h" -> "1H"
	if err != nil {
		return nil, err
	}

	u := fmt.Sprintf("https://www.okx.com/api/v5/market/candles?instId=%s&bar=%s&limit=%d",
		url.QueryEscape(instID), url.QueryEscape(bar), limit,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	var r struct {
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	if r.Code != "0" {
		return nil, fmt.Errorf("okx candles error: code=%s msg=%s", r.Code, r.Msg)
	}

	tfDur := timeframeToDuration(bar)

	// OKX обычно отдаёт newest-first → разворачиваем, чтобы прогрев шёл по времени
	out := make([]models.CandleTick, 0, len(r.Data))
	for i := len(r.Data) - 1; i >= 0; i-- {
		row := r.Data[i]
		if len(row) < 5 {
			continue
		}

		tsMs, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			continue
		}
		open, _ := strconv.ParseFloat(row[1], 64)
		high, _ := strconv.ParseFloat(row[2], 64)
		low, _ := strconv.ParseFloat(row[3], 64)
		closep, _ := strconv.ParseFloat(row[4], 64)
		if closep <= 0 {
			continue
		}

		start := time.UnixMilli(tsMs)
		end := start.Add(tfDur)

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

	return out, nil
}
