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

func (c *Client) PlaceSingleAlgo(
	ctx context.Context,
	instId string,
	posSide string,
	size float64,
	triggerPx float64,
	isTP bool,
) (string, error) { // ✅ algoId

	// 1. Сторона закрывающего ордера
	var side string
	switch strings.ToLower(posSide) {
	case "long":
		side = "sell"
	case "short":
		side = "buy"
	default:
		return "", fmt.Errorf("PlaceSingleAlgo: unsupported posSide=%q", posSide)
	}

	if size <= 0 {
		return "", fmt.Errorf("PlaceSingleAlgo: size <= 0")
	}
	if triggerPx <= 0 {
		return "", fmt.Errorf("PlaceSingleAlgo: triggerPx <= 0")
	}

	body := map[string]string{
		"instId":  instId,
		"tdMode":  "cross",
		"side":    side,
		"posSide": posSide,
		"ordType": "conditional",
		"sz":      formatSize(size),
	}

	if isTP {
		body["tpTriggerPx"] = formatPrice(triggerPx)
		body["tpOrdPx"] = "-1"
		body["tpTriggerPxType"] = "last"
	} else {
		body["slTriggerPx"] = formatPrice(triggerPx)
		body["slOrdPx"] = "-1"
		body["slTriggerPxType"] = "last"
	}

	payload, err := sonic.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("PlaceSingleAlgo marshal: %w", err)
	}

	const requestPath = "/api/v5/trade/order-algo"

	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, http.MethodPost, requestPath, string(payload))

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://www.okx.com"+requestPath,
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", fmt.Errorf("PlaceSingleAlgo new request: %w", err)
	}

	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("PlaceSingleAlgo do: %w", err)
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("PlaceSingleAlgo http %d: %s", resp.StatusCode, string(data))
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

	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("PlaceSingleAlgo decode: %w; body=%s", err, string(data))
	}

	// детальный статус
	if len(r.Data) > 0 && r.Data[0].SCode != "0" {
		return "", fmt.Errorf(
			"PlaceSingleAlgo algo rejected: sCode=%s sMsg=%s RAW=%s",
			r.Data[0].SCode, r.Data[0].SMsg, string(data),
		)
	}

	// общий код
	if r.Code != "0" {
		return "", fmt.Errorf(
			"PlaceSingleAlgo error: code=%s msg=%s RAW=%s",
			r.Code, r.Msg, string(data),
		)
	}

	if len(r.Data) == 0 || r.Data[0].AlgoId == "" {
		return "", fmt.Errorf("PlaceSingleAlgo: empty algoId RAW=%s", string(data))
	}

	return r.Data[0].AlgoId, nil
}
