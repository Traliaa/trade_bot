package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (c *Client) GetInstrumentMeta(
	ctx context.Context,
	instID string,
) (lastPx, lotSz, minSz, tickSz, ctVal float64, err error) {
	// 1. Берём только нужный инструмент, а не весь список SWAP
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://www.okx.com/api/v5/public/instruments?instType=SWAP&instId="+instID,
		nil,
	)
	if err != nil {
		err = fmt.Errorf("build request: %w", err)
		return
	}

	resp, err := c.http.Do(req)
	if err != nil {
		err = fmt.Errorf("do request: %w", err)
		return
	}
	defer resp.Body.Close()

	var payload struct {
		Code string       `json:"code"`
		Msg  string       `json:"msg"`
		Data []Instrument `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		err = fmt.Errorf("decode: %w", err)
		return
	}
	if payload.Code != "0" {
		err = fmt.Errorf("okx error %s: %s", payload.Code, payload.Msg)
		return
	}
	if len(payload.Data) == 0 {
		err = fmt.Errorf("instrument %s not found", instID)
		return
	}

	inst := payload.Data[0]

	// 2. lotSz / minSz
	if lotSz, err = strconv.ParseFloat(inst.LotSz, 64); err != nil {
		err = fmt.Errorf("parse lotSz: %w", err)
		return
	}
	if minSz, err = strconv.ParseFloat(inst.MinSz, 64); err != nil {
		err = fmt.Errorf("parse minSz: %w", err)
		return
	}

	// 3. tickSz
	if inst.TickSz != "" {
		if tickSz, err = strconv.ParseFloat(inst.TickSz, 64); err != nil {
			err = fmt.Errorf("parse tickSz: %w", err)
			return
		}
	} else {
		// если вдруг пустой — считаем, что tickSz неограничен
		tickSz = 0
	}

	// 4. ctVal (номинал контракта)
	if inst.CtVal != "" {
		if ctVal, err = strconv.ParseFloat(inst.CtVal, 64); err != nil {
			err = fmt.Errorf("parse ctVal: %w", err)
			return
		}
	} else {
		// безопасный дефолт, чтобы не делить на 0
		ctVal = 1
	}

	// 5. Последняя цена из тикера
	var px float64
	px, err = c.getLastPrice(ctx, instID)
	if err != nil {
		err = fmt.Errorf("ticker: %w", err)
		return
	}
	lastPx = px

	return
}
