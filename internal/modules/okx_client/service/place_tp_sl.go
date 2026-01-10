package service

//// PlaceTpsl ставит TP/SL на позицию через OKX /api/v5/trade/order-algo (ordType=tpsl).
//// instID  — например "BTC-USDT-SWAP"
//// posSide — "long" или "short"
//// sl, tp  — уровни стоп-лосса и тейк-профита по последней цене
//func (c *Client) PlaceTpsl(
//	ctx context.Context,
//	instId string,
//	posSide string, // "long"/"short"
//	size float64,
//	sl float64,
//	tp float64,
//) (string, error) { // <-- вернуть algoId
//
//	side := "sell"
//	if posSide == "short" {
//		side = "buy"
//	}
//
//	body := map[string]string{
//		"instId":  instId,
//		"tdMode":  "cross",
//		"side":    side,
//		"posSide": posSide,
//		"ordType": "conditional",
//		"sz":      formatSize(size),
//	}
//
//	if sl > 0 {
//		body["slTriggerPx"] = formatPrice(sl)
//		body["slOrdPx"] = "-1"
//		body["slTriggerPxType"] = "last"
//	}
//	if tp > 0 {
//		body["tpTriggerPx"] = formatPrice(tp)
//		body["tpOrdPx"] = "-1"
//		body["tpTriggerPxType"] = "last"
//	}
//
//	payload, err := sonic.Marshal(body)
//	if err != nil {
//		return "", fmt.Errorf("marshal tpsl: %w", err)
//	}
//
//	requestPath := "/api/v5/trade/order-algo"
//	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
//	sign := c.sign(ts, http.MethodPost, requestPath, string(payload))
//
//	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.okx.com"+requestPath, bytes.NewReader(payload))
//	if err != nil {
//		return "", fmt.Errorf("new request: %w", err)
//	}
//	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
//	req.Header.Set("OK-ACCESS-SIGN", sign)
//	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
//	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)
//	req.Header.Set("Content-Type", "application/json")
//
//	resp, err := c.http.Do(req)
//	if err != nil {
//		return "", err
//	}
//	defer resp.Body.Close()
//
//	data, _ := io.ReadAll(resp.Body)
//
//	var r struct {
//		Code string `json:"code"`
//		Msg  string `json:"msg"`
//		Data []struct {
//			AlgoId string `json:"algoId"`
//			SCode  string `json:"sCode"`
//			SMsg   string `json:"sMsg"`
//		} `json:"data"`
//	}
//	_ = json.Unmarshal(data, &r)
//
//	if r.Code != "0" {
//		return "", fmt.Errorf("okx algo error: %s %s", r.Code, r.Msg)
//	}
//	if len(r.Data) == 0 {
//		return "", fmt.Errorf("algo empty response: %s", string(data))
//	}
//	if r.Data[0].SCode != "0" {
//		return "", fmt.Errorf("algo reject: sCode=%s sMsg=%s", r.Data[0].SCode, r.Data[0].SMsg)
//	}
//
//	return r.Data[0].AlgoId, nil
//}
