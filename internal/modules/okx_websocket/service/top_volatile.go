package service

import (
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/universe"
)

type okxTicker struct {
	InstID    string `json:"instId"`
	Last      string `json:"last"`
	Open24h   string `json:"open24h"`
	High24h   string `json:"high24h"`
	Low24h    string `json:"low24h"`
	VolCcy24h string `json:"volCcy24h"`
	AskPx     string `json:"askPx"`
	BidPx     string `json:"bidPx"`
	Ts        string `json:"ts"`
}
type instrument struct {
	Lever    string `json:"lever"`
	InstID   string `json:"instId"`
	State    string `json:"state"`
	ListTime string `json:"listTime"`
	CtVal    string `json:"ctVal"`
	CtValCcy string `json:"ctValCcy"`
	LotSz    string `json:"lotSz"`
	MinSz    string `json:"minSz"`
}

func (s *Service) marketJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("market metadata HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(dst)
}
func (s *Service) universePolicy(limit int, mode models.UniverseMode) universe.Policy {
	l := models.LimitsForMode(mode)
	p := universe.Policy{Limit: limit, MinQuoteVolume: 10_000_000, MaxSpreadBPS: 20, MaxRangePct: l.MaxRangePct, MaxMovePct: l.MaxMovePct, MinAge: 30 * 24 * time.Hour, MaxAge: 2 * time.Minute, MinResidence: 6 * time.Hour, RetainBonus: .15}
	p.MinQuoteVolume = l.MinQuoteVolumeUSDT
	if s.cfg != nil {
		c := s.cfg.Strategy.Universe
		if c.TotalLimit != 0 {
			p.Limit = c.TotalLimit
		}
		if c.MinQuoteVolume != 0 {
			p.MinQuoteVolume = c.MinQuoteVolume
		}
		if c.MaxSpreadBPS != 0 {
			p.MaxSpreadBPS = c.MaxSpreadBPS
		}
		if c.MinListingDays != 0 {
			p.MinAge = time.Duration(c.MinListingDays) * 24 * time.Hour
		}
		if c.MinResidence != 0 {
			p.MinResidence = c.MinResidence
		}
		if c.RetainBonus != 0 {
			p.RetainBonus = c.RetainBonus
		}
	}
	return p
}
func (s *Service) rankedUniverse(ctx context.Context, limit int, mode models.UniverseMode) (universe.Result, error) {
	var ts struct {
		Code string      `json:"code"`
		Data []okxTicker `json:"data"`
	}
	var ms struct {
		Code string       `json:"code"`
		Data []instrument `json:"data"`
	}
	if err := s.marketJSON(ctx, s.endpoint+"/tickers?instType=SWAP", &ts); err != nil {
		return universe.Result{}, err
	}
	if err := s.marketJSON(ctx, strings.TrimSuffix(s.endpoint, "/market")+"/public/instruments?instType=SWAP", &ms); err != nil {
		return universe.Result{}, err
	}
	if ts.Code != "0" || ms.Code != "0" {
		return universe.Result{}, fmt.Errorf("market metadata rejected")
	}
	meta := map[string]instrument{}
	for _, m := range ms.Data {
		meta[m.InstID] = m
	}
	num := func(v string) float64 { n, _ := strconv.ParseFloat(v, 64); return n }
	stamp := func(v string) time.Time {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return time.Time{}
		}
		return time.UnixMilli(n)
	}
	cs := []universe.Candidate{}
	for _, t := range ts.Data {
		if !isTradeableUSDTSwap(t.InstID) {
			continue
		}
		m := meta[t.InstID]
		cs = append(cs, universe.Candidate{Symbol: t.InstID, Last: num(t.Last), Open: num(t.Open24h), High: num(t.High24h), Low: num(t.Low24h), BaseVolume: num(t.VolCcy24h), Bid: num(t.BidPx), Ask: num(t.AskPx), At: stamp(t.Ts), ListedAt: stamp(m.ListTime), Live: m.State == "live" && m.CtValCcy == strings.TrimSuffix(t.InstID, "-USDT-SWAP"), ContractValue: num(m.CtVal), LotSize: num(m.LotSz), MinSize: num(m.MinSz)})
		cs[len(cs)-1].MaxLeverage = num(m.Lever)
	}
	s.mu.RLock()
	held := map[string]time.Time{}
	for k, v := range s.selectedSince {
		held[k] = v
	}
	s.mu.RUnlock()
	return universe.Select(time.Now(), cs, s.universePolicy(limit, mode), held)
}
func (s *Service) TopVolatile(n int, mode models.UniverseMode) ([]string, error) {
	return s.SelectUniverse(n, mode)
}
func (s *Service) SelectUniverse(total int, mode models.UniverseMode) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	r, err := s.rankedUniverse(ctx, total, mode)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(r.Selected))
	for _, c := range r.Selected {
		result = append(result, c.Symbol)
	}
	if s.Logger != nil {
		s.Logger.Info("universe selected", zap.Int("total_limit", s.universePolicy(total, mode).Limit), zap.Int("selected", len(result)), zap.Any("rejected", r.Rejected))
	}
	return result, nil
}
