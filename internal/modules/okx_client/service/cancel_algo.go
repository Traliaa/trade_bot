package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bytedance/sonic"
)

func (c *Client) CancelAlgo(ctx context.Context, instId, algoId string) error {
	body := []map[string]string{{"instId": instId, "algoId": algoId}}
	payload, _ := sonic.Marshal(body)

	const requestPath = "/api/v5/trade/cancel-algos"
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, http.MethodPost, requestPath, string(payload))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.okx.com"+requestPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("CancelAlgo new request: %w", err)
	}

	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("CancelAlgo do: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("CancelAlgo http %d: %s", resp.StatusCode, string(data))
	}

	var r struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			AlgoId string `json:"algoId"`
			SCode  string `json:"sCode"`
			SMsg   string `json:"sMsg"`
		} `json:"data"`
	}
	_ = json.Unmarshal(data, &r)

	if r.Code != "0" {
		return fmt.Errorf("CancelAlgo error: code=%s msg=%s RAW=%s", r.Code, r.Msg, string(data))
	}
	if len(r.Data) == 0 || r.Data[0].SCode != "0" {
		return fmt.Errorf("CancelAlgo reject RAW=%s", string(data))
	}
	return nil
}

func (c *Client) CloseMarket(ctx context.Context, instID, posSide string, size float64) (string, error) {
	if size <= 0 {
		return "", fmt.Errorf("CloseMarket: size <= 0")
	}

	side := "sell" // закрываем long
	if posSide == "short" {
		side = "buy" // закрываем short
	}

	bodyMap := map[string]any{
		"instId":     instID,
		"tdMode":     "cross",
		"side":       side,
		"posSide":    posSide,
		"ordType":    "market",
		"sz":         formatSize(size),
		"reduceOnly": true,
	}

	bodyBytes, _ := json.Marshal(bodyMap)
	bodyStr := string(bodyBytes)

	requestPath := "/api/v5/trade/order"
	method := "POST"
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, method, requestPath, bodyStr)

	req, _ := http.NewRequestWithContext(ctx, method, "https://www.okx.com"+requestPath, strings.NewReader(bodyStr))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("CloseMarket http %d: %s", resp.StatusCode, string(rb))
	}

	var wrap struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			OrdID string `json:"ordId"`
			SCode string `json:"sCode"`
			SMsg  string `json:"sMsg"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &wrap); err != nil {
		return "", err
	}
	if len(wrap.Data) == 0 {
		return "", fmt.Errorf("CloseMarket: empty data code=%s msg=%s", wrap.Code, wrap.Msg)
	}
	d := wrap.Data[0]
	if wrap.Code != "0" || d.SCode != "0" {
		return "", fmt.Errorf("CloseMarket okx error: code=%s msg=%s sCode=%s sMsg=%s", wrap.Code, wrap.Msg, d.SCode, d.SMsg)
	}
	return d.OrdID, nil
}
