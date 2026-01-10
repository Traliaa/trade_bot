package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
