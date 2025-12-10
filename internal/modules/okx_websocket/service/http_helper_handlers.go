package service

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

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

// ===== REST: top volatile SWAP instruments (OKX) =====

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
