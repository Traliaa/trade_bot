package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
) (string, error) {

	if size <= 0 {
		return "", fmt.Errorf("PlaceSingleAlgo: size <= 0")
	}

	if triggerPx <= 0 {
		return "", fmt.Errorf("PlaceSingleAlgo: triggerPx <= 0")
	}

	var side string

	switch strings.ToLower(posSide) {
	case "long":
		side = "sell"
	case "short":
		side = "buy"
	default:
		return "", fmt.Errorf("unsupported posSide %q", posSide)
	}

	sz := formatSize(size)

	body := map[string]string{
		"instId":     instId,
		"tdMode":     "cross",
		"side":       side,
		"posSide":    posSide,
		"ordType":    "conditional",
		"sz":         sz,
		"reduceOnly": "true",
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

	log.Printf(
		"[OKX ALGO ORDER] inst=%s posSide=%s side=%s size=%.8f trigger=%.8f isTP=%t",
		instId,
		posSide,
		side,
		size,
		triggerPx,
		isTP,
	)

	payload, _ := sonic.Marshal(body)

	const requestPath = "/api/v5/trade/order-algo"

	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, http.MethodPost, requestPath, string(payload))

	req, _ := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://www.okx.com"+requestPath,
		bytes.NewReader(payload),
	)

	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("okx http %d: %s", resp.StatusCode, string(data))
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
		return "", err
	}

	if len(r.Data) == 0 {
		return "", fmt.Errorf("empty algo response: %s", string(data))
	}

	d := r.Data[0]

	if r.Code != "0" || d.SCode != "0" {
		return "", fmt.Errorf(
			"okx algo error: code=%s msg=%s sCode=%s sMsg=%s",
			r.Code, r.Msg, d.SCode, d.SMsg,
		)
	}

	return d.AlgoId, nil
}
