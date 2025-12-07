package exchange

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bytedance/sonic"
	"github.com/gorilla/websocket"
)

type Client struct {
	mu        sync.RWMutex
	prices    map[string]float64
	http      *http.Client
	wsDialer  *websocket.Dialer
	apiKey    string
	apiSecret string
	passph    string
}

func NewClient() *Client {
	return &Client{
		prices:   make(map[string]float64),
		http:     &http.Client{Timeout: 10 * time.Second},
		wsDialer: &websocket.Dialer{},
	}
}

// SetCreds — сюда теперь кладём ключи OKX (пока с старыми env-именами MEXC_*)
func (c *Client) SetCreds(key, secret, passphrase string) {
	c.apiKey = key
	c.apiSecret = secret
	c.passph = passphrase
}

func (c *Client) SetPrice(symbol string, price float64) {
	c.mu.Lock()
	c.prices[symbol] = price
	c.mu.Unlock()
}

// ===== WebSocket: last price per instrument (OKX public tickers) =====

func (c *Client) StreamPrices(ctx context.Context, instID string) <-chan float64 {
	ch := make(chan float64)
	go func() {
		defer close(ch)

		url := "wss://ws.okx.com:8443/ws/v5/public"
		retry := 0

		for {
			conn, _, err := c.wsDialer.Dial(url, nil)
			if err != nil {
				retry++
				if retry > 8 {
					return
				}
				time.Sleep(time.Duration(300*retry) * time.Millisecond)
				continue
			}
			retry = 0

			sub := map[string]any{
				"op": "subscribe",
				"args": []map[string]string{{
					"channel": "tickers",
					"instId":  instID,
				}},
			}
			_ = conn.WriteJSON(sub)

			// пингуем, чтобы соединение жило
			stopPing := make(chan struct{})
			go func() {
				t := time.NewTicker(20 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-stopPing:
						return
					case <-ctx.Done():
						return
					case <-t.C:
						_ = conn.WriteJSON(map[string]string{"op": "ping"})
					}
				}
			}()

			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					close(stopPing)
					_ = conn.Close()
					break
				}

				var frame struct {
					Arg struct {
						Channel string `json:"channel"`
						InstID  string `json:"instId"`
					} `json:"arg"`
					Data []struct {
						Last string `json:"last"`
					} `json:"data"`
				}
				if err := json.Unmarshal(msg, &frame); err != nil {
					continue
				}
				if frame.Arg.Channel != "tickers" || len(frame.Data) == 0 {
					continue
				}
				p, err := strconv.ParseFloat(frame.Data[0].Last, 64)
				if err != nil || p == 0 {
					continue
				}
				c.SetPrice(instID, p)
				ch <- p
			}

			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(1 * time.Second)
			}
		}
	}()
	return ch
}

// ===== REST: top volatile SWAP instruments (OKX) =====

func (c *Client) TopVolatile(n int) []string {
	if n <= 0 {
		return nil
	}

	// все swap-инструменты
	tickers, err := c.fetchSwapTickers()
	if err != nil || len(tickers) == 0 {
		return nil
	}

	type rec struct {
		sym   string
		score float64
	}

	arr := make([]rec, 0, len(tickers))
	for _, t := range tickers {
		// берём только USDT-perp SWAP, вида BTC-USDT-SWAP
		if !strings.HasSuffix(t.InstID, "-USDT-SWAP") {
			continue
		}

		last, err1 := strconv.ParseFloat(t.Last, 64)
		high, err2 := strconv.ParseFloat(t.High24h, 64)
		low, err3 := strconv.ParseFloat(t.Low24h, 64)
		if err1 != nil || err2 != nil || err3 != nil || last <= 0 {
			continue
		}
		range24 := high - low
		if range24 <= 0 {
			continue
		}
		score := range24 / last
		arr = append(arr, rec{sym: t.InstID, score: score})
	}

	if len(arr) == 0 {
		return nil
	}

	sort.Slice(arr, func(i, j int) bool { return arr[i].score > arr[j].score })
	if n > len(arr) {
		n = len(arr)
	}
	res := make([]string, 0, n)
	for i := 0; i < n; i++ {
		res = append(res, arr[i].sym)
	}
	return res
}

type okxTicker struct {
	InstType string `json:"instType"`
	InstID   string `json:"instId"`
	Last     string `json:"last"`
	High24h  string `json:"high24h"`
	Low24h   string `json:"low24h"`
}

type okxTickerResp struct {
	Code string      `json:"code"`
	Msg  string      `json:"msg"`
	Data []okxTicker `json:"data"`
}

func (c *Client) fetchSwapTickers() ([]okxTicker, error) {
	req, _ := http.NewRequest("GET", "https://www.okx.com/api/v5/market/tickers?instType=SWAP", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("non-2xx: %d %s", resp.StatusCode, string(body))
	}

	body, _ := io.ReadAll(resp.Body)
	var wrap okxTickerResp
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	if wrap.Code != "0" {
		return nil, fmt.Errorf("okx error: code=%s msg=%s", wrap.Code, wrap.Msg)
	}
	return wrap.Data, nil
}

// ===== Private trading: place market order on OKX =====

// SetLeverage — выставляет плечо для инструмента на OKX.
// lever = 3, mgnMode сейчас "cross", posSide "long"/"short" или "" (для обоих).
func (c *Client) SetLeverage(ctx context.Context, instID string, lever int, posSide string) error {

	bodyMap := map[string]any{
		"instId":  instID,
		"mgnMode": "cross", // можно потом сделать параметром
		"lever":   strconv.Itoa(lever),
	}
	if posSide != "" {
		bodyMap["posSide"] = posSide
	}

	bodyBytes, _ := json.Marshal(bodyMap)
	bodyStr := string(bodyBytes)

	requestPath := "/api/v5/account/set-leverage"
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
		return err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d (set-leverage): %s", resp.StatusCode, string(rb))
	}

	var wrap struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []any  `json:"data"`
	}
	if err := json.Unmarshal(rb, &wrap); err != nil {
		return err
	}
	if wrap.Code != "0" {
		return fmt.Errorf("okx set-leverage error: code=%s msg=%s", wrap.Code, wrap.Msg)
	}
	return nil
}

// PlaceMarket — маршаллируем в /api/v5/trade/order
// side: 1 = открыть long, 3 = открыть short (как было в старой логике)
// leverage и openType пока не используем, режим маржи фиксируем через tdMode.
// PlaceMarket — маркет-ордер на OKX с установкой плеча и TP/SL.
func (c *Client) PlaceMarket(
	ctx context.Context,
	instID string,
	vol float64,
	side, leverage, openType int,
) (string, error) {
	if c.apiKey == "" || c.apiSecret == "" || c.passph == "" {
		return "", errors.New("okx creds empty (ключ/секрет/пасфраза)")
	}

	var sideStr, posSide string
	switch side {
	case 1:
		sideStr, posSide = "buy", "long"
	case 3:
		sideStr, posSide = "sell", "short"
	default:
		return "", fmt.Errorf("unsupported side %d", side)
	}

	// размер: vol как количество контрактов
	sz := fmt.Sprintf("%.0f", vol)
	if vol < 1 {
		sz = "1"
	}

	// сначала best-effort выставляем плечо
	if leverage > 0 {
		_ = c.SetLeverage(ctx, instID, leverage, posSide)
	}

	bodyMap := map[string]any{
		"instId":  instID,
		"tdMode":  "cross",
		"side":    sideStr,
		"posSide": posSide,
		"ordType": "market",
		"sz":      sz,
	}

	// ⚠️ ВАЖНО: здесь НЕТ tp/sl полей, чтобы избежать 54070

	bodyBytes, _ := json.Marshal(bodyMap)
	bodyStr := string(bodyBytes)

	requestPath := "/api/v5/trade/order"
	method := "POST"
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, method, requestPath, bodyStr)

	req, _ := http.NewRequestWithContext(
		ctx,
		method,
		"https://www.okx.com"+requestPath,
		strings.NewReader(bodyStr),
	)
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
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(rb))
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
		return "", fmt.Errorf("okx trade error: code=%s msg=%s (empty data)", wrap.Code, wrap.Msg)
	}
	d := wrap.Data[0]
	if wrap.Code != "0" || d.SCode != "0" {
		return "", fmt.Errorf(
			"okx trade error: code=%s msg=%s sCode=%s sMsg=%s",
			wrap.Code, wrap.Msg, d.SCode, d.SMsg,
		)
	}
	return d.OrdID, nil
}

func (c *Client) USDTBalance(ctx context.Context) (float64, error) {
	if c.apiKey == "" || c.apiSecret == "" || c.passph == "" {
		return 0, errors.New("okx creds empty (ключ/секрет/пасфраза)")
	}

	requestPath := "/api/v5/account/balance?ccy=USDT"
	method := "GET"
	bodyStr := ""
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, method, requestPath, bodyStr)

	req, _ := http.NewRequestWithContext(ctx, method, "https://www.okx.com"+requestPath, nil)
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return 0, fmt.Errorf("http %d (balance): %s", resp.StatusCode, string(rb))
	}

	var wrap struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			TotalEq string `json:"totalEq"`
			Details []struct {
				Ccy     string `json:"ccy"`
				Eq      string `json:"eq"`
				AvailEq string `json:"availEq"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rb, &wrap); err != nil {
		return 0, err
	}
	if wrap.Code != "0" || len(wrap.Data) == 0 {
		return 0, fmt.Errorf("okx balance error: code=%s msg=%s", wrap.Code, wrap.Msg)
	}

	// сначала пытаемся взять availEq по USDT
	for _, d := range wrap.Data[0].Details {
		if d.Ccy != "USDT" {
			continue
		}
		if d.AvailEq != "" {
			if v, err := strconv.ParseFloat(d.AvailEq, 64); err == nil {
				return v, nil
			}
		}
		if d.Eq != "" {
			if v, err := strconv.ParseFloat(d.Eq, 64); err == nil {
				return v, nil
			}
		}
	}

	// fallback: totalEq
	if wrap.Data[0].TotalEq != "" {
		if v, err := strconv.ParseFloat(wrap.Data[0].TotalEq, 64); err == nil {
			return v, nil
		}
	}
	return 0, errors.New("okx balance: USDT not found")
}

func formatPx(v float64) string {
	// под себя: количество знаков после запятой можно взять из метаданных инструмента
	return strconv.FormatFloat(v, 'f', -1, 64)
}

type placeAlgoResp struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		AlgoId string `json:"algoId"`
		SCode  string `json:"sCode"`
		SMsg   string `json:"sMsg"`
	} `json:"data"`
}

// PlaceTpsl ставит TP/SL на позицию через OKX /api/v5/trade/order-algo (ordType=tpsl).
// instID  — например "BTC-USDT-SWAP"
// posSide — "long" или "short"
// sl, tp  — уровни стоп-лосса и тейк-профита по последней цене
func (c *Client) PlaceTpsl(
	ctx context.Context,
	instId string,
	posSide string, // "long" / "short"
	size float64,
	sl float64,
	tp float64,
) error {

	side := "sell"
	if posSide == "short" {
		side = "buy"
	}

	body := map[string]string{
		"instId":  instId,
		"tdMode":  "cross",
		"side":    side,
		"posSide": posSide,
		"ordType": "conditional",
		"sz":      formatSize(size),
	}

	if sl > 0 {
		body["slTriggerPx"] = formatPrice(sl)
		body["slOrdPx"] = "-1"
		body["slTriggerPxType"] = "last"
	}

	if tp > 0 {
		body["tpTriggerPx"] = formatPrice(tp)
		body["tpOrdPx"] = "-1"
		body["tpTriggerPxType"] = "last" // ← ОБЯЗАТЕЛЬНО
	}

	payload, err := sonic.Marshal(body) // ВАЖНО: без [] вокруг!
	if err != nil {
		return fmt.Errorf("marshal tpsl: %w", err)
	}

	requestPath := "/api/v5/trade/order-algo"
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, http.MethodPost, requestPath, string(payload))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://www.okx.com"+requestPath, // если у тебя другая базовая URL, вставь свою
		bytes.NewReader(payload),
	)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	var r struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			AlgoId string `json:"algoId"`
			SCode  string `json:"sCode"`
			SMsg   string `json:"sMsg"`
		} `json:"data"`
	}
	json.Unmarshal(data, &r)

	if r.Code != "0" {
		return fmt.Errorf("okx algo error: %s %s", r.Code, r.Msg)
	}
	if len(r.Data) == 0 {
		return fmt.Errorf("algo empty response: %s", string(data))
	}
	if r.Data[0].SCode != "0" {
		return fmt.Errorf("algo reject: sCode=%s sMsg=%s", r.Data[0].SCode, r.Data[0].SMsg)
	}

	return nil
}
func formatPrice(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}

func formatSize(s float64) string {
	return strconv.FormatFloat(s, 'f', -1, 64)
}

func (c *Client) sign(ts, method, requestPath, body string) string {

	msg := ts + strings.ToUpper(method) + requestPath + body
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

type Instrument struct {
	InstID   string `json:"instId"`
	TickSz   string `json:"tickSz"`
	LotSz    string `json:"lotSz"`
	MinSz    string `json:"minSz"`
	CtVal    string `json:"ctVal"`
	CtMult   string `json:"ctMult"`
	State    string `json:"state"`
	MaxMktSz string `json:"maxMktSz"`
}

func (c *Client) GetInstrumentMeta(ctx context.Context, instID string) (
	price float64,
	stepSize float64,
	minSz float64,
	tickSize float64,
	maxMktSz float64,
	err error,
) {
	// 1. Получаем инструменты
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.okx.com/api/v5/public/instruments?instType=SWAP",
		nil)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Code string       `json:"code"`
		Msg  string       `json:"msg"`
		Data []Instrument `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("decode: %w", err)
	}

	if data.Code != "0" {
		return 0, 0, 0, 0, 0, fmt.Errorf("okx error %s: %s", data.Code, data.Msg)
	}

	// 2. Находим нужный инструмент
	var inst *Instrument
	for i := range data.Data {
		if data.Data[i].InstID == instID {
			inst = &data.Data[i]
			break
		}
	}

	if inst == nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("instrument %s not found", instID)
	}

	// 3. Парсим шаги и лимиты
	stepSize, err = strconv.ParseFloat(inst.LotSz, 64)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("parse lotSz: %w", err)
	}

	minSz, err = strconv.ParseFloat(inst.MinSz, 64)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("parse minSz: %w", err)
	}

	// 4. Берём цену инструмента из tickers
	tkPrice, err := c.getLastPrice(ctx, instID)
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("ticker: %w", err)
	}
	// 4. Парсим шаг цены (tickSz)
	if inst.TickSz != "" {
		tickSize, err = strconv.ParseFloat(inst.TickSz, 64)
		if err != nil {
			err = fmt.Errorf("parse tickSz: %w", err)
			return
		}
	} else {
		// на всякий случай — если tickSz по какой-то причине пустой
		tickSize = 0
	}
	maxMktSz, _ = strconv.ParseFloat(inst.MaxMktSz, 64)

	return tkPrice, stepSize, minSz, tickSize, maxMktSz, nil
}

func (c *Client) getLastPrice(ctx context.Context, instID string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.okx.com/api/v5/market/ticker?instId="+instID,
		nil)
	if err != nil {
		return 0, fmt.Errorf("build ticker request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("execute ticker request: %w", err)
	}
	defer resp.Body.Close()

	var data struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Last string `json:"last"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, fmt.Errorf("decode ticker: %w", err)
	}

	if data.Code != "0" || len(data.Data) == 0 {
		return 0, fmt.Errorf("ticker error %s: %s", data.Code, data.Msg)
	}

	price, err := strconv.ParseFloat(data.Data[0].Last, 64)
	if err != nil {
		return 0, fmt.Errorf("parse last price: %w", err)
	}

	return price, nil
}

//// StreamCandles — поток закрытых свечей OKX по таймфрейму ("1m","5m","15m").
//// На выход отдаём цену закрытия последней закрытой свечи.
//func (m *Client) StreamCandles(ctx context.Context, instID, timeframe string) <-chan float64 {
//	ch := make(chan float64)
//	go func() {
//		defer close(ch)
//
//		channel := "candle" + timeframe
//		url := "wss://ws.okx.com:8443/ws/v5/public"
//
//		for {
//			log.Printf("[WS] connect %s %s", channel, instID)
//			conn, _, err := m.wsDialer.Dial(url, nil)
//			if err != nil {
//				log.Printf("[WS] dial error %s %s: %v", channel, instID, err)
//				time.Sleep(time.Second)
//				continue
//			}
//
//			sub := map[string]any{
//				"op": "subscribe",
//				"args": []map[string]string{{
//					"channel": channel,
//					"instId":  instID,
//				}},
//			}
//			if err := conn.WriteJSON(sub); err != nil {
//				log.Printf("[WS] subscribe error %s %s: %v", channel, instID, err)
//				_ = conn.Close()
//				continue
//			}
//
//			// 🔴 вот это новое — ping каждые 20s
//			stopPing := make(chan struct{})
//			go func() {
//				defer close(stopPing)
//				t := time.NewTicker(20 * time.Second)
//				defer t.Stop()
//				for {
//					select {
//					case <-ctx.Done():
//						return
//					case <-stopPing:
//						return
//					case <-t.C:
//						_ = conn.WriteJSON(map[string]string{"op": "ping"})
//					}
//				}
//			}()
//
//			for {
//				_, msg, err := conn.ReadMessage()
//				if err != nil {
//					log.Printf("[WS] read error %s %s: %v", channel, instID, err)
//					_ = conn.Close()
//					break
//				}
//
//				var frame struct {
//					Arg struct {
//						Channel string `json:"channel"`
//						InstID  string `json:"instId"`
//					} `json:"arg"`
//					Data []struct {
//						Last string `json:"last"`
//					} `json:"data"`
//				}
//				if err := json.Unmarshal(msg, &frame); err != nil {
//					continue
//				}
//				if frame.Arg.Channel != "tickers" || len(frame.Data) == 0 {
//					continue
//				}
//				p, err := strconv.ParseFloat(frame.Data[0].Last, 64)
//				if err != nil || p == 0 {
//					continue
//				}
//				m.SetPrice(instID, p)
//				ch <- p
//			}
//
//			select {
//			case <-ctx.Done():
//				return
//			default:
//				time.Sleep(time.Second)
//			}
//		}
//	}()
//	return ch
//}

// Проверка: доступны ли свечи для инструмента
func (c *Client) HasCandles(instID, tf string) bool {
	url := fmt.Sprintf("https://www.okx.com/api/v5/market/candles?instId=%s&bar=%s", instID, tf)

	req, _ := http.NewRequest("GET", url, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return false
	}

	var wrap struct {
		Code string     `json:"code"`
		Msg  string     `json:"msg"`
		Data [][]string `json:"data"`
	}
	b, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(b, &wrap); err != nil {
		return false
	}

	if wrap.Code != "0" || len(wrap.Data) == 0 {
		return false
	}
	return true
}
