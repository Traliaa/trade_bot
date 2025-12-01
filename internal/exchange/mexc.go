package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type MexcClient struct {
	mu        sync.RWMutex
	prices    map[string]float64
	http      *http.Client
	wsDialer  *websocket.Dialer
	apiKey    string
	apiSecret string
}

func NewMexcClient() *MexcClient {
	return &MexcClient{
		prices:   make(map[string]float64),
		http:     &http.Client{Timeout: 10 * time.Second},
		wsDialer: &websocket.Dialer{},
	}
}

func (m *MexcClient) SetCreds(key, secret string) { m.apiKey, m.apiSecret = key, secret }
func (m *MexcClient) SetPrice(symbol string, price float64) {
	m.mu.Lock()
	m.prices[symbol] = price
	m.mu.Unlock()
}
func (m *MexcClient) GetPrice(symbol string) float64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.prices[symbol]
}

// ===== WS: last price per symbol =====

func (m *MexcClient) StreamPrices(ctx context.Context, symbol string) <-chan float64 {
	ch := make(chan float64)
	go func() {
		defer close(ch)
		url := "wss://contract.mexc.com/edge"
		retry := 0
		for {
			conn, _, err := m.wsDialer.Dial(url, nil)
			if err != nil {
				retry++
				if retry > 8 {
					return
				}
				time.Sleep(time.Duration(300*retry) * time.Millisecond)
				continue
			}
			retry = 0
			_ = conn.WriteJSON(map[string]any{"method": "sub.ticker", "param": map[string]string{"symbol": symbol}})

			stopPing := make(chan struct{})
			go func() {
				t := time.NewTicker(15 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-stopPing:
						return
					case <-ctx.Done():
						return
					case <-t.C:
						_ = conn.WriteJSON(map[string]string{"method": "ping"})
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
					Channel string `json:"channel"`
					Data    struct {
						Last float64 `json:"lastPrice"`
					} `json:"data"`
				}
				if err := json.Unmarshal(msg, &frame); err == nil && frame.Channel == "push.ticker" {
					if frame.Data.Last != 0 {
						m.SetPrice(symbol, frame.Data.Last)
						ch <- frame.Data.Last
					}
				}
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

// ===== REST: топ волатильных контрактов =====

func (m *MexcClient) TopVolatile(n int) []string {
	if n <= 0 {
		return nil
	}

	tickers, err := m.fetchFuturesTickers()
	if err != nil || len(tickers) == 0 {
		return nil
	}

	type rec struct {
		sym   string
		score float64
	}
	arr := make([]rec, 0, len(tickers))
	for _, t := range tickers {
		if !strings.HasSuffix(t.Symbol, "_USDT") {
			continue
		}
		if t.LastPrice <= 0 {
			continue
		}
		range24 := t.High24 - t.Low24
		if range24 <= 0 {
			continue
		}
		score := range24 / t.LastPrice
		arr = append(arr, rec{sym: t.Symbol, score: score})
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

func (m *MexcClient) fetchFuturesTickers() ([]futuresTicker, error) {
	req, _ := http.NewRequest("GET", "https://contract.mexc.com/api/v1/contract/ticker", nil)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, errors.New("non-2xx status")
	}
	body, _ := io.ReadAll(resp.Body)
	var wrap futuresTickerResp
	if err := json.Unmarshal(body, &wrap); err != nil {
		return nil, err
	}
	if !wrap.Success {
		return nil, errors.New("mexc: success=false")
	}
	var arr []futuresTicker
	if err := json.Unmarshal(wrap.Data, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}
	var one futuresTicker
	if err := json.Unmarshal(wrap.Data, &one); err == nil && one.Symbol != "" {
		return []futuresTicker{one}, nil
	}
	return nil, errors.New("unexpected data shape")
}

type futuresTicker struct {
	Symbol     string  `json:"symbol"`
	LastPrice  float64 `json:"lastPrice"`
	High24     float64 `json:"high24Price"`
	Low24      float64 `json:"lower24Price"`
	ChangeRate float64 `json:"riseFallRate"`
}

type futuresTickerResp struct {
	Success bool            `json:"success"`
	Code    int             `json:"code"`
	Data    json.RawMessage `json:"data"`
}

// ===== Private: открытие ордеров и чтение позиций =====

func (m *MexcClient) sign(accessKey, secret, reqTime, paramString string) string {
	s := accessKey + reqTime + paramString
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// Market order
func (m *MexcClient) PlaceMarket(ctx context.Context, symbol string, vol float64, side, leverage, openType int) (string, error) {
	if m.apiKey == "" || m.apiSecret == "" {
		return "", errors.New("api creds empty")
	}
	body := map[string]any{
		"symbol":   symbol,
		"price":    0,
		"vol":      vol,
		"type":     5,
		"side":     side,
		"openType": openType,
		"leverage": leverage,
	}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://contract.mexc.com/api/v1/private/order/create", strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	reqTime := time.Now().UTC().UnixMilli()
	paramStr := string(b)
	sig := m.sign(m.apiKey, m.apiSecret, fmt.Sprintf("%d", reqTime), paramStr)
	req.Header.Set("ApiKey", m.apiKey)
	req.Header.Set("Request-Time", fmt.Sprintf("%d", reqTime))
	req.Header.Set("Signature", sig)

	resp, err := m.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(rb))
	}
	var wrap struct {
		Success bool `json:"success"`
		Code    int  `json:"code"`
		Data    struct {
			OrderID string `json:"orderId"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rb, &wrap); err != nil {
		return "", err
	}
	if !wrap.Success {
		return "", fmt.Errorf("mexc error: code=%d msg=%s", wrap.Code, wrap.Message)
	}
	return wrap.Data.OrderID, nil
}

// Структура открытой позиции (упрощённо).
type OpenPosition struct {
	PositionID   int64   `json:"positionId"`
	Symbol       string  `json:"symbol"`
	PositionType int     `json:"positionType"` // 1 long, 2 short
	OpenType     int     `json:"openType"`     // 1 isolated, 2 cross
	HoldVol      float64 `json:"holdVol"`
	HoldAvgPrice float64 `json:"holdAvgPrice"`
	Leverage     int     `json:"leverage"`
	MarginRatio  float64 `json:"marginRatio"`
	Realised     float64 `json:"realised"`
}

// GET /api/v1/private/position/open_positions
func (m *MexcClient) OpenPositions(ctx context.Context) ([]OpenPosition, error) {
	if m.apiKey == "" || m.apiSecret == "" {
		return nil, errors.New("api creds empty")
	}

	req, _ := http.NewRequestWithContext(ctx, "https://contract.mexc.com/api/v1/private/position/open_positions", nil)

	reqTime := time.Now().UTC().UnixMilli()
	paramStr := "" // без параметров
	sig := m.sign(m.apiKey, m.apiSecret, fmt.Sprintf("%d", reqTime), paramStr)

	req.Header.Set("ApiKey", m.apiKey)
	req.Header.Set("Request-Time", fmt.Sprintf("%d", reqTime))
	req.Header.Set("Signature", sig)

	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, string(rb))
	}

	var wrap struct {
		Success bool           `json:"success"`
		Code    int            `json:"code"`
		Data    []OpenPosition `json:"data"`
		Message string         `json:"message"`
	}
	if err := json.Unmarshal(rb, &wrap); err != nil {
		return nil, err
	}
	if !wrap.Success {
		return nil, fmt.Errorf("mexc error: code=%d msg=%s", wrap.Code, wrap.Message)
	}
	return wrap.Data, nil
}
