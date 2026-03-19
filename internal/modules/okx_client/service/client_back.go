package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"trade_bot/internal/models"
)

// OpenPositions вытаскивает открытые позиции с OKX и мапит их в упрощённую структуру
// для использования в Telegram-нотифайере (команда /positions).
func (c *Client) OpenPositions(ctx context.Context) ([]models.OpenPosition, error) {
	resp, err := c.http.Do(c.generateRequest(ctx, http.MethodGet, "/api/v5/account/positions", ""))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(rb))
	}

	respData := OpenPositionsResponse{}
	if err := json.Unmarshal(rb, &respData); err != nil {
		return nil, err
	}
	if respData.Code != "0" {
		return nil, fmt.Errorf("okx positions error: code=%s msg=%s", respData.Code, respData.Msg)
	}

	res := make([]models.OpenPosition, 0, len(respData.Data))

	for _, d := range respData.Data {
		posContracts, _ := strconv.ParseFloat(d.Pos, 64)
		avgPx, _ := strconv.ParseFloat(d.AvgPx, 64)

		lastPx, _ := strconv.ParseFloat(d.Last, 64)
		if lastPx == 0 {
			lastPx, _ = strconv.ParseFloat(d.MarkPx, 64)
		}

		upl, _ := strconv.ParseFloat(d.UplLastPx, 64)
		if upl == 0 {
			upl, _ = strconv.ParseFloat(d.Upl, 64)
		}

		uplRatio, _ := strconv.ParseFloat(d.UplRatioLastPx, 64)
		if uplRatio == 0 {
			uplRatio, _ = strconv.ParseFloat(d.UplRatio, 64)
		}
		uplPct := uplRatio * 100.0

		realised, _ := strconv.ParseFloat(d.RealizedPnl, 64)
		lev, _ := strconv.Atoi(d.Lever)

		side := "long"
		pt := 1
		if d.PosSide == "short" {
			side = "short"
			pt = 2
		}

		avgPx2, _ := strconv.ParseFloat(d.AvgPx, 64)
		markPx, _ := strconv.ParseFloat(d.MarkPx, 64)
		last, _ := strconv.ParseFloat(d.Last, 64)

		// подтягиваем ctVal
		var ctVal float64
		meta, err := c.GetInstrumentMeta(ctx, d.InstId)
		if err == nil {
			ctVal = meta.CtVal
		}

		baseQty := posContracts
		if ctVal > 0 {
			baseQty = posContracts * ctVal
		}

		res = append(res, models.OpenPosition{
			Symbol:       d.InstId,
			PositionType: pt,
			HoldVol:      posContracts, // contracts
			Leverage:     lev,
			Realised:     realised,

			Size:      posContracts, // оставить временно как contracts
			Contracts: posContracts,
			BaseQty:   baseQty,
			CtVal:     ctVal,

			HoldAvgPrice:  avgPx,
			EntryPrice:    avgPx,
			LastPrice:     lastPx,
			UnrealizedPnL: upl,
			UnrealizedPct: uplPct,
			Side:          side,
			AvgPx:         avgPx2,
			MarkPx:        markPx,
			Last:          last,
		})
	}

	return res, nil
}
func (c *Client) generateRequest(ctx context.Context, method string, requestPath string, body string) *http.Request {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	msg := ts + strings.ToUpper(method) + requestPath + body
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(msg))
	req, _ := http.NewRequestWithContext(ctx, method, "https://www.okx.com"+requestPath, nil)
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", base64.StdEncoding.EncodeToString(h.Sum(nil)))
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)
	return req
}
