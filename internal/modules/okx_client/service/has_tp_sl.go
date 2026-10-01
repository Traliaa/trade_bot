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
	AlgoID      string `json:"algoId"`
	Side        string `json:"side"`
	Size        string `json:"sz"`
	SLType      string `json:"slTriggerPxType"`
	SLOrdPx     string `json:"slOrdPx"`
	TPOrdPx     string `json:"tpOrdPx"`
	ReduceOnly  string `json:"reduceOnly"`
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
	items, err := c.listAlgoPendingType(ctx, instID, orderType)
	if err != nil {
		return false, false, err
	}
	for _, it := range items {
		if it.State != "live" || strings.TrimSpace(it.InstId) != strings.TrimSpace(instID) || !strings.EqualFold(strings.TrimSpace(it.PosSide), strings.TrimSpace(posSide)) {
			continue
		}
		hasTP = hasTP || validTriggerPrice(it.TpTriggerPx)
		hasSL = hasSL || validTriggerPrice(it.SlTriggerPx)
	}
	return hasTP, hasSL, nil
}

func (c *Client) listAlgoPendingType(ctx context.Context, instID, orderType string) ([]algoPendingItem, error) {
	q := url.Values{}
	q.Set("instType", "SWAP")
	q.Set("instId", instID)
	q.Set("ordType", orderType)
	q.Set("limit", "100")

	path := "/api/v5/trade/orders-algo-pending?" + q.Encode()

	resp, err := c.http.Do(c.generateRequest(ctx, http.MethodGet, path, ""))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rb, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(rb))
	}

	var data algoPendingResp
	if err := json.Unmarshal(rb, &data); err != nil {
		return nil, err
	}
	if data.Code != "0" {
		return nil, fmt.Errorf("okx pending algo error: code=%s msg=%s", data.Code, data.Msg)
	}
	if len(data.Data) >= 100 {
		return nil, fmt.Errorf("pending protective orders response may be truncated for %s", instID)
	}
	return data.Data, nil
}

// ProtectiveOrder exposes enough exchange evidence to reconcile protection,
// not merely its presence. OCO orders retain both legs under a single ID.
type ProtectiveOrder struct {
	ID, OrderType, SLType string
	Size, SL, TP          float64
}

func (c *Client) PendingProtection(ctx context.Context, instID, posSide string) ([]ProtectiveOrder, error) {
	if posSide != "long" && posSide != "short" {
		return nil, fmt.Errorf("runner requires explicit hedge position side")
	}
	var result []ProtectiveOrder
	for _, kind := range []string{"conditional", "oco"} {
		items, err := c.listAlgoPendingType(ctx, instID, kind)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if it.State != "live" || it.InstId != instID || it.PosSide != posSide {
				continue
			}
			if (it.SlTriggerPx == "" || it.SlTriggerPx == "0") && (it.TpTriggerPx == "" || it.TpTriggerPx == "0") {
				continue
			}
			wantSide := "sell"
			if posSide == "short" {
				wantSide = "buy"
			}
			// In OKX hedge mode long/sell and short/buy are always closing
			// orders; reduceOnly is a net-mode control, not a hedge guarantee.
			if it.AlgoID == "" || it.Side != wantSide || !validTriggerPrice(it.Size) {
				return nil, fmt.Errorf("unrecognized protective order for %s", instID)
			}
			order := ProtectiveOrder{ID: it.AlgoID, OrderType: kind, SLType: it.SLType}
			order.Size, _ = strconv.ParseFloat(it.Size, 64)
			if it.SlTriggerPx != "" && it.SlTriggerPx != "0" {
				if !validTriggerPrice(it.SlTriggerPx) || it.SLOrdPx != "-1" || (it.SLType != "last" && it.SLType != "") {
					return nil, fmt.Errorf("unsupported stop semantics for %s", instID)
				}
				order.SL, _ = strconv.ParseFloat(it.SlTriggerPx, 64)
			}
			if it.TpTriggerPx != "" && it.TpTriggerPx != "0" {
				if !validTriggerPrice(it.TpTriggerPx) {
					return nil, fmt.Errorf("invalid TP for %s", instID)
				}
				order.TP, _ = strconv.ParseFloat(it.TpTriggerPx, 64)
			}
			result = append(result, order)
		}
	}
	return result, nil
}

func validTriggerPrice(raw string) bool {
	p, err := strconv.ParseFloat(raw, 64)
	return err == nil && p > 0 && !math.IsInf(p, 0) && !math.IsNaN(p)
}
