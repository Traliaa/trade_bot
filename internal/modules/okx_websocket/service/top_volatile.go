package service

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"trade_bot/internal/models"

	"github.com/bytedance/sonic"
	"go.uber.org/zap"
)

type okxTicker struct {
	InstType string `json:"instType"`
	InstID   string `json:"instId"`

	Last   string `json:"last"`
	LastSz string `json:"lastSz"`

	AskPx string `json:"askPx"`
	AskSz string `json:"askSz"`
	BidPx string `json:"bidPx"`
	BidSz string `json:"bidSz"`

	Open24h string `json:"open24h"`
	High24h string `json:"high24h"`
	Low24h  string `json:"low24h"`

	VolCcy24h string `json:"volCcy24h"`
	Vol24h    string `json:"vol24h"`

	Ts      string `json:"ts"`
	SodUTC0 string `json:"sodUtc0"`
	SodUTC8 string `json:"sodUtc8"`
}

type FetchSwapTickersResp struct {
	Code string      `json:"code"`
	Msg  string      `json:"msg"`
	Data []okxTicker `json:"data"`
}

func (s *Service) TopVolatile(n int, mode models.UniverseMode) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}

	limits := models.LimitsForMode(mode)

	req, err := http.NewRequest(
		http.MethodGet,
		fmt.Sprintf("%s/tickers?instType=SWAP", s.endpoint),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("make request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("okx http error: status=%d body=%s", resp.StatusCode, string(body))
	}

	var wrap FetchSwapTickersResp
	if err := sonic.Unmarshal(body, &wrap); err != nil {
		return nil, fmt.Errorf("unmarshal tickers: %w", err)
	}

	if wrap.Code != "0" {
		return nil, fmt.Errorf("okx error: code=%s msg=%s", wrap.Code, wrap.Msg)
	}

	if len(wrap.Data) == 0 {
		return nil, nil
	}

	type rec struct {
		sym      string
		score    float64
		rangePct float64
		movePct  float64
		volCcy   float64
		last     float64
	}

	arr := make([]rec, 0, len(wrap.Data))

	for _, t := range wrap.Data {
		if !isTradeableUSDTSwap(t.InstID) {
			continue
		}

		last, err1 := strconv.ParseFloat(t.Last, 64)
		open, err2 := strconv.ParseFloat(t.Open24h, 64)
		high, err3 := strconv.ParseFloat(t.High24h, 64)
		low, err4 := strconv.ParseFloat(t.Low24h, 64)
		volCcy, err5 := strconv.ParseFloat(t.VolCcy24h, 64)

		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}
		if last <= 0 || open <= 0 || high <= 0 || low <= 0 || volCcy <= 0 {
			continue
		}
		if high <= low {
			continue
		}

		rangePct := (high - low) / last
		movePct := math.Abs(last-open) / open

		// hard filters
		if last < limits.MinLast {
			continue
		}
		if volCcy < limits.MinVolCcy24 {
			continue
		}
		if rangePct > limits.MaxRangePct {
			continue
		}
		if movePct > limits.MaxMovePct {
			continue
		}

		// базовый score: волатильность + ликвидность
		liqScore := math.Pow(math.Log1p(volCcy), 1.35)
		score := rangePct * liqScore

		// мягкие штрафы внутри допустимого диапазона
		switch {
		case movePct > limits.MaxMovePct*0.85:
			score *= 0.75
		case movePct > limits.MaxMovePct*0.70:
			score *= 0.85
		}

		switch {
		case rangePct > limits.MaxRangePct*0.90:
			score *= 0.75
		case rangePct > limits.MaxRangePct*0.75:
			score *= 0.85
		}

		arr = append(arr, rec{
			sym:      t.InstID,
			score:    score,
			rangePct: rangePct,
			movePct:  movePct,
			volCcy:   volCcy,
			last:     last,
		})
	}

	sort.Slice(arr, func(i, j int) bool {
		if arr[i].score == arr[j].score {
			return arr[i].volCcy > arr[j].volCcy
		}
		return arr[i].score > arr[j].score
	})

	if n > len(arr) {
		n = len(arr)
	}

	res := make([]string, 0, n)
	for i := 0; i < n; i++ {
		res = append(res, arr[i].sym)
	}

	return res, nil
}

func (s *Service) SelectUniverse(dynamicN int, mode models.UniverseMode) ([]string, error) {
	core := coreSymbols()

	dyn, err := s.TopVolatile(dynamicN, mode)
	if err != nil {
		return nil, err
	}

	final := mergeUniqueSymbols(core, dyn)

	s.Logger.Info("universe selected",
		zap.String("mode", string(mode)),
		zap.Int("core_count", len(core)),
		zap.Int("dynamic_count", len(dyn)),
		zap.Int("final_count", len(final)),
		zap.Strings("core_symbols", core),
		zap.Strings("dynamic_top_symbols", firstN(dyn, 20)),
		zap.Strings("final_top_symbols", firstN(final, 30)),
	)

	return final, nil
}
