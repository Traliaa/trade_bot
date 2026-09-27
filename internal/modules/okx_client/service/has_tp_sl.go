package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type algoPendingItem struct {
	State       string `json:"state"`
	InstId      string `json:"instId"`
	PosSide     string `json:"posSide"`     // long/short
	SlTriggerPx string `json:"slTriggerPx"` // если не пусто/0 → есть SL
	TpTriggerPx string `json:"tpTriggerPx"` // если не пусто/0 → есть TP
}

type algoPendingResp struct {
	Code string            `json:"code"`
	Msg  string            `json:"msg"`
	Data []algoPendingItem `json:"data"`
}

// HasTpSl проверяет, есть ли на позиции выставленные TP и SL (pending algo orders).
func (c *Client) HasTpSl(ctx context.Context, instID, posSide string) (hasTP bool, hasSL bool, _ error) {
	for _, orderType := range []string{"conditional", "oco"} {
		tp, sl, err := c.hasTpSlType(ctx, instID, posSide, orderType)
		if err != nil {
			return false, false, err
		}
		hasTP = hasTP || tp
		hasSL = hasSL || sl
	}
	return hasTP, hasSL, nil
}

func (c *Client) hasTpSlType(ctx context.Context, instID, posSide, orderType string) (hasTP bool, hasSL bool, _ error) {
	q := url.Values{}
	q.Set("instType", "SWAP")
	q.Set("instId", instID)
	q.Set("ordType", orderType)
	q.Set("limit", "100")

	path := "/api/v5/trade/orders-algo-pending?" + q.Encode()

	resp, err := c.http.Do(c.generateRequest(ctx, http.MethodGet, path, ""))
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()

	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, false, err
	}
	if resp.StatusCode/100 != 2 {
		return false, false, fmt.Errorf("http %d: %s", resp.StatusCode, string(rb))
	}

	var data algoPendingResp
	if err := json.Unmarshal(rb, &data); err != nil {
		return false, false, err
	}
	if data.Code != "0" {
		return false, false, fmt.Errorf("okx pending algo error: code=%s msg=%s", data.Code, data.Msg)
	}
	if len(data.Data) >= 100 {
		return false, false, fmt.Errorf("pending protective orders response may be truncated for %s", instID)
	}

	wantSide := strings.ToLower(strings.TrimSpace(posSide))
	wantInst := strings.TrimSpace(instID)

	for _, it := range data.Data {
		if it.State != "live" {
			continue
		}
		if strings.TrimSpace(it.InstId) != wantInst {
			continue
		}
		if strings.ToLower(strings.TrimSpace(it.PosSide)) != wantSide {
			continue
		}
		if validTriggerPrice(it.TpTriggerPx) {
			hasTP = true
		}
		if validTriggerPrice(it.SlTriggerPx) {
			hasSL = true
		}
		if hasTP && hasSL {
			return true, true, nil
		}
	}
	return hasTP, hasSL, nil
}

func validTriggerPrice(raw string) bool {
	p, err := strconv.ParseFloat(raw, 64)
	return err == nil && p > 0 && !math.IsInf(p, 0) && !math.IsNaN(p)
}
