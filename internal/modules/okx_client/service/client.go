package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/models"

	"github.com/gorilla/websocket"
)

type Client struct {
	mu sync.RWMutex
	//prices    map[string]float64
	http      *http.Client
	wsDialer  *websocket.Dialer
	apiKey    string
	apiSecret string
	passph    string
}

func NewClient(cfg *models.UserSettings) *Client {
	return &Client{
		//prices:    make(map[string]float64),
		http:      &http.Client{Timeout: 10 * time.Second},
		wsDialer:  &websocket.Dialer{},
		apiKey:    cfg.Settings.TradingSettings.OKXAPIKey,
		apiSecret: cfg.Settings.TradingSettings.OKXAPISecret,
		passph:    cfg.Settings.TradingSettings.OKXPassphrase,
	}
}

func (c *Client) SetPrice(symbol string, price float64) {
	c.mu.Lock()
	//c.prices[symbol] = price
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
						_ = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
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

// ===== Private trading: place market order on OKX =====

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

func (c *Client) USDTBalance(ctx context.Context) (*models.AccountSnapshot, error) {
	if c.apiKey == "" || c.apiSecret == "" || c.passph == "" {
		return nil, errors.New("okx creds empty (ключ/секрет/пасфраза)")
	}

	requestPath := "/api/v5/account/balance?ccy=USDT"
	method := "GET"
	bodyStr := ""
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, method, requestPath, bodyStr)

	req, err := http.NewRequestWithContext(ctx, method, "https://www.okx.com"+requestPath, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d (balance): %s", resp.StatusCode, string(rb))
	}

	var wrap struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			TotalEq string `json:"totalEq"`
			Details []struct {
				Ccy       string `json:"ccy"`
				Eq        string `json:"eq"`
				AvailEq   string `json:"availEq"`
				FrozenBal string `json:"frozenBal"`
				Upl       string `json:"upl"`
			} `json:"details"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rb, &wrap); err != nil {
		return nil, err
	}
	if wrap.Code != "0" || len(wrap.Data) == 0 {
		return nil, fmt.Errorf("okx balance error: code=%s msg=%s", wrap.Code, wrap.Msg)
	}

	parse := func(v string) float64 {
		if v == "" {
			return 0
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0
		}
		return f
	}

	snap := &models.AccountSnapshot{
		TotalEquity: parse(wrap.Data[0].TotalEq),
		UpdatedAt:   time.Now().UTC(),
	}

	for _, d := range wrap.Data[0].Details {
		if !strings.EqualFold(d.Ccy, "USDT") {
			continue
		}

		snap.AvailableBalance = parse(d.AvailEq)
		if snap.AvailableBalance == 0 {
			snap.AvailableBalance = parse(d.Eq)
		}

		snap.FrozenBalance = parse(d.FrozenBal)
		snap.UnrealizedPnL = parse(d.Upl)

		// если totalEq пустой/нулевой, fallback на eq
		if snap.TotalEquity == 0 {
			snap.TotalEquity = parse(d.Eq)
		}

		return snap, nil
	}

	// fallback: если details без USDT, но totalEq есть
	if snap.TotalEquity > 0 {
		return snap, nil
	}

	return nil, errors.New("okx balance: USDT not found")
}
func (c *Client) RecentFills(ctx context.Context, instID string, limit int) ([]models.TradeFill, error) {
	if c.apiKey == "" || c.apiSecret == "" || c.passph == "" {
		return nil, errors.New("okx creds empty")
	}
	if limit <= 0 {
		limit = 20
	}

	requestPath := fmt.Sprintf("/api/v5/trade/fills?instId=%s&limit=%d", url.QueryEscape(instID), limit)
	method := "GET"
	bodyStr := ""

	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	sign := c.sign(ts, method, requestPath, bodyStr)

	req, err := http.NewRequestWithContext(ctx, method, "https://www.okx.com"+requestPath, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", sign)
	req.Header.Set("OK-ACCESS-TIMESTAMP", ts)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passph)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("http %d (fills): %s", resp.StatusCode, string(rb))
	}

	var wrap struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstID  string `json:"instId"`
			PosSide string `json:"posSide"`
			Side    string `json:"side"`
			FillPx  string `json:"fillPx"`
			FillSz  string `json:"fillSz"`
			Fee     string `json:"fee"`
			FillPnl string `json:"fillPnl"`
			TradeID string `json:"tradeId"`
			Ts      string `json:"ts"`
		} `json:"data"`
	}

	if err := json.Unmarshal(rb, &wrap); err != nil {
		return nil, err
	}
	if wrap.Code != "0" {
		return nil, fmt.Errorf("okx fills error: code=%s msg=%s", wrap.Code, wrap.Msg)
	}

	parseF := func(v string) float64 {
		if v == "" {
			return 0
		}
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}

	parseMs := func(v string) time.Time {
		if v == "" {
			return time.Time{}
		}
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return time.Time{}
		}
		return time.UnixMilli(ms).UTC()
	}

	out := make([]models.TradeFill, 0, len(wrap.Data))
	for _, d := range wrap.Data {
		out = append(out, models.TradeFill{
			InstID:      d.InstID,
			PosSide:     strings.ToLower(d.PosSide),
			Side:        strings.ToLower(d.Side),
			FillPx:      parseF(d.FillPx),
			FillSz:      parseF(d.FillSz),
			Fee:         parseF(d.Fee),
			RealizedPnL: parseF(d.FillPnl),
			TradeID:     d.TradeID,
			FillTime:    parseMs(d.Ts),
		})
	}

	return out, nil
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

//type T struct {
//	Code string `json:"code"`
//	Data []struct {
//		AdjEq              string `json:"adjEq"`
//		AvailEq            string `json:"availEq"`
//		BorrowFroz         string `json:"borrowFroz"`
//		Delta              string `json:"delta"`
//		DeltaLever         string `json:"deltaLever"`
//		DeltaNeutralStatus string `json:"deltaNeutralStatus"`
//		Details            []struct {
//			AccAvgPx              string `json:"accAvgPx"`
//			AutoLendAmt           string `json:"autoLendAmt"`
//			AutoLendMtAmt         string `json:"autoLendMtAmt"`
//			AutoLendStatus        string `json:"autoLendStatus"`
//			AutoStakingStatus     string `json:"autoStakingStatus"`
//			AvailBal              string `json:"availBal"`
//			AvailEq               string `json:"availEq"`
//			BorrowFroz            string `json:"borrowFroz"`
//			CashBal               string `json:"cashBal"`
//			Ccy                   string `json:"ccy"`
//			ClSpotInUseAmt        string `json:"clSpotInUseAmt"`
//			ColBorrAutoConversion string `json:"colBorrAutoConversion"`
//			ColRes                string `json:"colRes"`
//			CollateralEnabled     bool   `json:"collateralEnabled"`
//			CollateralRestrict    bool   `json:"collateralRestrict"`
//			CrossLiab             string `json:"crossLiab"`
//			DisEq                 string `json:"disEq"`
//			Eq                    string `json:"eq"`
//			EqUsd                 string `json:"eqUsd"`
//			FixedBal              string `json:"fixedBal"`
//			FrozenBal             string `json:"frozenBal"`
//			FrpType               string `json:"frpType"`
//			Imr                   string `json:"imr"`
//			Interest              string `json:"interest"`
//			IsoEq                 string `json:"isoEq"`
//			IsoLiab               string `json:"isoLiab"`
//			IsoUpl                string `json:"isoUpl"`
//			Liab                  string `json:"liab"`
//			MaxLoan               string `json:"maxLoan"`
//			MaxSpotInUse          string `json:"maxSpotInUse"`
//			MgnRatio              string `json:"mgnRatio"`
//			Mmr                   string `json:"mmr"`
//			NotionalLever         string `json:"notionalLever"`
//			OpenAvgPx             string `json:"openAvgPx"`
//			OrdFrozen             string `json:"ordFrozen"`
//			RewardBal             string `json:"rewardBal"`
//			SmtSyncEq             string `json:"smtSyncEq"`
//			SpotBal               string `json:"spotBal"`
//			SpotCopyTradingEq     string `json:"spotCopyTradingEq"`
//			SpotInUseAmt          string `json:"spotInUseAmt"`
//			SpotIsoBal            string `json:"spotIsoBal"`
//			SpotUpl               string `json:"spotUpl"`
//			SpotUplRatio          string `json:"spotUplRatio"`
//			StgyEq                string `json:"stgyEq"`
//			TotalPnl              string `json:"totalPnl"`
//			TotalPnlRatio         string `json:"totalPnlRatio"`
//			Twap                  string `json:"twap"`
//			UTime                 string `json:"uTime"`
//			Upl                   string `json:"upl"`
//			UplLiab               string `json:"uplLiab"`
//		} `json:"details"`
//		Imr                   string `json:"imr"`
//		IsoEq                 string `json:"isoEq"`
//		MgnRatio              string `json:"mgnRatio"`
//		Mmr                   string `json:"mmr"`
//		NotionalUsd           string `json:"notionalUsd"`
//		NotionalUsdForBorrow  string `json:"notionalUsdForBorrow"`
//		NotionalUsdForFutures string `json:"notionalUsdForFutures"`
//		NotionalUsdForOption  string `json:"notionalUsdForOption"`
//		NotionalUsdForSwap    string `json:"notionalUsdForSwap"`
//		OrdFroz               string `json:"ordFroz"`
//		TotalEq               string `json:"totalEq"`
//		UTime                 string `json:"uTime"`
//		Upl                   string `json:"upl"`
//	} `json:"data"`
//	Msg string `json:"msg"`
//}
