package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type algoPendingItem struct {
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
	q := url.Values{}
	q.Set("instType", "SWAP")
	// можно НЕ фильтровать по instId, OKX вернёт все pending, мы отфильтруем локально
	// q.Set("instId", instID)

	path := "/api/v5/trade/orders-algo-pending?" + q.Encode()

	resp, err := c.http.Do(c.generateRequest(ctx, http.MethodGet, path, ""))
	if err != nil {
		return false, false, err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
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

	wantSide := strings.ToLower(strings.TrimSpace(posSide))
	wantInst := strings.TrimSpace(instID)

	for _, it := range data.Data {
		if strings.TrimSpace(it.InstId) != wantInst {
			continue
		}
		if strings.ToLower(strings.TrimSpace(it.PosSide)) != wantSide {
			continue
		}
		if it.TpTriggerPx != "" && it.TpTriggerPx != "0" {
			hasTP = true
		}
		if it.SlTriggerPx != "" && it.SlTriggerPx != "0" {
			hasSL = true
		}
		if hasTP && hasSL {
			return true, true, nil
		}
	}
	return hasTP, hasSL, nil
}
