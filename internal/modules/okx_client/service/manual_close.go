package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

type ManualCloseRejected struct{ Code string }

func (e *ManualCloseRejected) Error() string {
	return "Биржа отклонила ордер: " + e.Code
}

// ClosingPosition intentionally supports only the hedge/cross mode used by
// this bot. Do not infer a closing side from an unsupported net position.
func (c *Client) ClosingPosition(ctx context.Context, instID, side string) (float64, error) {
	req := c.generateRequest(ctx, http.MethodGet, "/api/v5/account/positions?instId="+url.QueryEscape(instID), "")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var data OpenPositionsResponse
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("positions HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	if data.Code != "0" {
		return 0, fmt.Errorf("positions code %s", data.Code)
	}
	for _, p := range data.Data {
		if p.InstId == instID && p.PosSide == side {
			if p.MgnMode != "cross" {
				return 0, fmt.Errorf("поддерживаются только cross-позиции")
			}
			return strconv.ParseFloat(p.Pos, 64)
		}
	}
	return 0, nil
}

func (c *Client) SubmitManualClose(ctx context.Context, instID, side string, size float64, clientID string) (string, error) {
	if side != "long" && side != "short" {
		return "", fmt.Errorf("invalid position side")
	}
	orderSide := "sell"
	if side == "short" {
		orderSide = "buy"
	}
	body, _ := json.Marshal(map[string]any{"instId": instID, "tdMode": "cross", "side": orderSide, "posSide": side, "ordType": "market", "sz": formatSize(size), "reduceOnly": true, "clOrdId": clientID})
	req := c.generateRequest(ctx, http.MethodPost, "/api/v5/trade/order", string(body))
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var data struct {
		Code string `json:"code"`
		Data []struct {
			OrderID string `json:"ordId"`
			Code    string `json:"sCode"`
		} `json:"data"`
	}
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("order HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	if len(data.Data) == 1 && data.Data[0].Code != "" && data.Data[0].Code != "0" {
		return "", &ManualCloseRejected{Code: data.Data[0].Code}
	}
	if data.Code != "0" || len(data.Data) != 1 || data.Data[0].Code != "0" || data.Data[0].OrderID == "" {
		return "", fmt.Errorf("биржа не подтвердила ордер: code=%s", data.Code)
	}
	return data.Data[0].OrderID, nil
}

func (c *Client) ManualCloseState(ctx context.Context, instID, clientID string) (string, string, error) {
	req := c.generateRequest(ctx, http.MethodGet, "/api/v5/trade/order?instId="+url.QueryEscape(instID)+"&clOrdId="+url.QueryEscape(clientID), "")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var data struct {
		Code string `json:"code"`
		Data []struct {
			State   string `json:"state"`
			OrderID string `json:"ordId"`
		} `json:"data"`
	}
	if resp.StatusCode/100 != 2 {
		return "", "", fmt.Errorf("order lookup HTTP %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", err
	}
	if data.Code != "0" || len(data.Data) != 1 {
		return "", "", fmt.Errorf("ордер пока не найден")
	}
	return data.Data[0].State, data.Data[0].OrderID, nil
}
