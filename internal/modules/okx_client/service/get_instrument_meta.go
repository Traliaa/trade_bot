package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"trade_bot/internal/models"
)

func (c *Client) GetInstrumentMeta(ctx context.Context, instID string) (models.Instrument, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://www.okx.com/api/v5/public/instruments?instType=SWAP&instId="+url.QueryEscape(instID),
		nil,
	)
	if err != nil {
		return models.Instrument{}, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return models.Instrument{}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return models.Instrument{}, fmt.Errorf("http %d: %s", resp.StatusCode, string(b))
	}

	var payload struct {
		Code string       `json:"code"`
		Msg  string       `json:"msg"`
		Data []Instrument `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return models.Instrument{}, fmt.Errorf("decode: %w", err)
	}
	if payload.Code != "0" {
		return models.Instrument{}, fmt.Errorf("okx error %s: %s", payload.Code, payload.Msg)
	}
	if len(payload.Data) == 0 {
		return models.Instrument{}, fmt.Errorf("instrument %s not found", instID)
	}

	inst := payload.Data[0]
	if inst.State != "" && inst.State != "live" {
		return models.Instrument{}, fmt.Errorf("instrument %s not live: state=%s", instID, inst.State)
	}

	parsePos := func(name, s string) (float64, error) {
		if s == "" {
			return 0, fmt.Errorf("%s empty", name)
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil || v <= 0 {
			return 0, fmt.Errorf("%s parse: %v (%q)", name, err, s)
		}
		return v, nil
	}

	lotSz, err := parsePos("lotSz", inst.LotSz)
	if err != nil {
		return models.Instrument{}, err
	}
	minSz, err := parsePos("minSz", inst.MinSz)
	if err != nil {
		return models.Instrument{}, err
	}
	tickSz, err := parsePos("tickSz", inst.TickSz)
	if err != nil {
		return models.Instrument{}, err
	}
	ctValBase, err := parsePos("ctVal", inst.CtVal)
	if err != nil {
		return models.Instrument{}, err
	}

	ctMult := 1.0
	if inst.CtMult != "" {
		if v, e := strconv.ParseFloat(inst.CtMult, 64); e == nil && v > 0 {
			ctMult = v
		}
	}
	ctValEff := ctValBase * ctMult

	var maxMktSz float64
	if inst.MaxMktSz != "" {
		maxMktSz, _ = strconv.ParseFloat(inst.MaxMktSz, 64)
	}

	lastPx, err := c.getLastPrice(ctx, instID)
	if err != nil {
		return models.Instrument{}, fmt.Errorf("ticker: %w", err)
	}
	if lastPx <= 0 {
		return models.Instrument{}, fmt.Errorf("lastPx <= 0: %.10f", lastPx)
	}

	kind := models.ContractUnknown
	switch strings.ToLower(strings.TrimSpace(inst.CtType)) {
	case "linear":
		// типично settleCcy=USDT и ctValCcy=USDT
		kind = models.ContractLinearUSDT
	case "inverse":
		kind = models.ContractInverseCoin
	}

	return models.Instrument{
		InstID:    inst.InstID,
		Kind:      kind,
		SettleCcy: inst.SettleCcy,
		CtValCcy:  inst.CtValCcy,

		LastPx:   lastPx,
		LotSz:    lotSz,
		MinSz:    minSz,
		TickSz:   tickSz,
		CtVal:    ctValEff,
		MaxMktSz: maxMktSz,
	}, nil
}

func (c *Client) SettleCcyToUSDT(ctx context.Context, settleCcy string) (float64, error) {
	s := strings.ToUpper(strings.TrimSpace(settleCcy))
	if s == "" {
		return 0, fmt.Errorf("empty settleCcy")
	}
	if s == "USDT" {
		return 1, nil
	}

	// самый простой вариант: берем тикер по споту.
	// Если у тебя getLastPrice работает только по SWAP instId — сделай отдельный getSpotLastPrice.
	instID := s + "-USDT"
	px, err := c.getLastPrice(ctx, instID) // <-- реализуй через /api/v5/market/ticker?instId=BTC-USDT
	if err != nil {
		return 0, err
	}
	if px <= 0 {
		return 0, fmt.Errorf("settle px <= 0: %s %.10f", instID, px)
	}
	return px, nil
}
