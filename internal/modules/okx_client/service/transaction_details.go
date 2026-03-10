package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"trade_bot/internal/models"
)

func (c *Client) TransactionDetails(ctx context.Context, instType, instID string, limit int) ([]models.TransactionDetailRecord, error) {
	q := url.Values{}
	q.Set("instType", instType)
	if instID != "" {
		q.Set("instId", instID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}

	path := "/api/v5/trade/fills-history"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	resp, err := c.http.Do(c.generateRequest(ctx, http.MethodGet, path, ""))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(body))
	}

	var out models.TransactionDetailsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.Code != "0" {
		return nil, fmt.Errorf("okx fills history error: code=%s msg=%s", out.Code, out.Msg)
	}

	return out.Data, nil
}
