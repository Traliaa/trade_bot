package research

import (
	"fmt"
	"go.uber.org/zap"
	"math"
	"sort"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/universe"
)

type event struct {
	at              time.Time
	priority, index int
	symbol          string
}
type pending struct {
	setup Setup
	at    time.Time
}

func direction(s models.Side) float64 {
	if s == models.SideSell {
		return -1
	}
	return 1
}
func validBar(c models.CandleTick) bool {
	for _, v := range []float64{c.Open, c.High, c.Low, c.Close} {
		if !universe.FinitePositive(v) {
			return false
		}
	}
	return c.InstID != "" && !c.Start.IsZero() && c.End.After(c.Start) && c.Low <= math.Min(c.Open, c.Close) && c.High >= math.Max(c.Open, c.Close) && c.Volume >= 0 && !math.IsNaN(c.Volume) && !math.IsInf(c.Volume, 0)
}
func Run(d Dataset, o Options) (Report, error) {
	r := Report{Options: o, Trades: []Trade{}, Funnel: map[string]int{}, Warnings: []string{"Research execution model: fixed stop/target and time exit; production trailing/partial exits are not replicated.", "Intra-minute SL/TP ambiguity is resolved stop-first; funding uses supplied historical mark prices."}}
	if err := o.Policy.Validate(); err != nil {
		return r, err
	}
	if d.Schema != 1 || d.Provenance == "" || len(d.Bars) == 0 || len(d.Snapshots) == 0 {
		return r, fmt.Errorf("schema=1, provenance, closed bars and historical snapshots required")
	}
	if o.Strategy != "v3" && o.Strategy != "smc" {
		return r, fmt.Errorf("unknown research strategy")
	}
	if !o.End.After(o.Start) || o.MaxPositions < 1 || o.MaxHolding <= 0 || o.SnapshotMaxAge <= 0 {
		return r, fmt.Errorf("invalid period or limits")
	}
	for _, v := range []float64{o.Equity, o.RiskPct, o.Leverage, o.RR} {
		if !universe.FinitePositive(v) {
			return r, fmt.Errorf("invalid execution value")
		}
	}
	if o.RiskPct > 1 || o.Leverage > 20 || o.FeeBPS < 0 || o.SlippageBPS < 0 || o.FeeBPS > 100 || o.SlippageBPS > 100 || math.IsNaN(o.FeeBPS) || math.IsNaN(o.SlippageBPS) {
		return r, fmt.Errorf("unsafe or invalid research costs/risk")
	}
	if !d.FundingComplete {
		r.Warnings = append(r.Warnings, "Funding coverage is incomplete: results are not eligible for live promotion.")
	}
	cfg := &config.Config{Strategy: d.Strategy}
	cfg.Strategy.Name = "donchian_v3_smart"
	if cfg.Strategy.LTF == "" {
		cfg.Strategy.LTF = "15m"
	}
	if cfg.Strategy.HTF == "" {
		cfg.Strategy.HTF = "1h"
	}
	cfg.Strategy.ApplyV3Defaults()
	cfg.Strategy.V3.AllowShorts = o.Shorts
	r.EffectiveStrategy = cfg.Strategy
	r.Warnings = append(r.Warnings, "Each evaluation window starts with fresh equity and no positions; windows are not a continuous live equity curve.")
	ltf, htf := helper.NormTF(cfg.Strategy.LTF), helper.NormTF(cfg.Strategy.HTF)
	if ltf == htf || ltf == "1m" || htf == "1m" {
		return r, fmt.Errorf("distinct 1m execution, LTF and HTF required")
	}
	engine := strategy.NewService(cfg, nil, nil)
	engine.Base = base.New("replay", zap.NewNop(), false)
	smc := NewSMC()
	ls := map[string][]models.CandleTick{}
	hs := map[string][]models.CandleTick{}
	es := []event{}
	seen := map[string]bool{}
	haveMinute := false
	for i, c := range d.Bars {
		if !validBar(c) {
			return r, fmt.Errorf("invalid bar %d", i)
		}
		tf := helper.NormTF(c.TimeframeRaw)
		duration, err := time.ParseDuration(tf)
		if err != nil || c.End.Sub(c.Start) != duration {
			return r, fmt.Errorf("inconsistent candle duration for %s", tf)
		}
		key := c.InstID + "/" + tf + "/" + c.Start.Format(time.RFC3339Nano)
		if seen[key] {
			return r, fmt.Errorf("duplicate bar %s", key)
		}
		seen[key] = true
		p := 4
		switch tf {
		case "1m":
			p = 1
			haveMinute = true
			if c.End.Sub(c.Start) != time.Minute {
				return r, fmt.Errorf("execution candle is not 1m")
			}
			es = append(es, event{c.Start, 5, i, c.InstID})
		case htf:
			p = 3
		case ltf:
			p = 4
		default:
			return r, fmt.Errorf("unsupported timeframe %s", tf)
		}
		es = append(es, event{c.End, p, i, c.InstID})
	}
	if !haveMinute {
		return r, fmt.Errorf("1m execution history required")
	}
	snapshotTimes := map[time.Time]bool{}
	for i, s := range d.Snapshots {
		if snapshotTimes[s.At] {
			return r, fmt.Errorf("duplicate universe snapshot")
		}
		snapshotTimes[s.At] = true
		if s.At.IsZero() {
			return r, fmt.Errorf("snapshot timestamp required")
		}
		es = append(es, event{s.At, 0, i, ""})
	}
	fundingKeys := map[string]bool{}
	for i, f := range d.Funding {
		key := f.Symbol + "/" + f.At.Format(time.RFC3339Nano)
		if fundingKeys[key] {
			return r, fmt.Errorf("duplicate funding event")
		}
		fundingKeys[key] = true
		if f.At.IsZero() || !universe.FinitePositive(f.Mark) || math.IsNaN(f.Rate) || math.IsInf(f.Rate, 0) {
			return r, fmt.Errorf("invalid funding record")
		}
		es = append(es, event{f.At, 2, i, f.Symbol})
	}
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if !a.at.Equal(b.at) {
			return a.at.Before(b.at)
		}
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		return a.symbol < b.symbol
	})
	selected := map[string]universe.Ranked{}
	held := map[string]time.Time{}
	var snapshotAt time.Time
	positions := map[string]*Trade{}
	orders := map[string]pending{}
	last := map[string]models.CandleTick{}
	balance, peak := o.Equity, o.Equity
	markEquity := func() {
		equity := balance
		for sym, p := range positions {
			mark := p.Entry
			if c, ok := last[sym]; ok {
				mark = c.Close
			}
			equity += direction(p.Side) * (mark - p.Entry) * p.Contracts * p.ContractValue
		}
		peak = math.Max(peak, equity)
		r.MaxDrawdown = math.Max(r.MaxDrawdown, peak-equity)
	}
	closeTrade := func(sym string, price float64, at time.Time, reason string) {
		p := positions[sym]
		price *= 1 - direction(p.Side)*(o.SlippageBPS+p.SpreadBPS/2)/10000
		p.Exit = price
		p.CloseAt = at
		p.Reason = reason
		p.Gross = direction(p.Side) * (price - p.Entry) * p.Contracts * p.ContractValue
		fee := price * p.Contracts * p.ContractValue * o.FeeBPS / 10000
		p.Fees += fee
		p.Net = p.Gross - p.Fees + p.Funding
		balance += p.Gross - fee
		r.Turnover += price * p.Contracts * p.ContractValue
		r.Trades = append(r.Trades, *p)
		delete(positions, sym)
		markEquity()
	}
	for _, ev := range es {
		if ev.at.After(o.End) {
			break
		}
		switch ev.priority {
		case 0:
			snap := d.Snapshots[ev.index]
			res, err := universe.Select(snap.At, snap.Candidates, o.Policy, held)
			if err != nil {
				return r, err
			}
			selected = map[string]universe.Ranked{}
			nextHeld := map[string]time.Time{}
			for _, v := range res.Selected {
				selected[v.Symbol] = v
				at := held[v.Symbol]
				if at.IsZero() {
					at = snap.At
				}
				nextHeld[v.Symbol] = at
			}
			held = nextHeld
			snapshotAt = snap.At
			if !ev.at.Before(o.Start) {
				r.Funnel["snapshot_selected"] += len(selected)
				for k, n := range res.Rejected {
					r.Funnel["universe_"+k] += n
				}
			}
		case 2:
			f := d.Funding[ev.index]
			if p := positions[f.Symbol]; p != nil {
				cash := -direction(p.Side) * f.Rate * f.Mark * p.Contracts * p.ContractValue
				p.Funding += cash
				balance += cash
				markEquity()
			}
		case 5:
			c := d.Bars[ev.index]
			if c.Start.Before(o.Start) || !c.Start.Before(o.End) {
				continue
			}
			order, ok := orders[c.InstID]
			if !ok {
				continue
			}
			delete(orders, c.InstID)
			if c.Start.Before(order.at) || c.Start.Sub(order.at) > time.Minute {
				r.Funnel["expired_signal"]++
				continue
			}
			if positions[c.InstID] != nil {
				r.Funnel["position_exists"]++
				continue
			}
			m, ok := selected[c.InstID]
			if !ok || snapshotAt.IsZero() || c.Start.Sub(snapshotAt) > o.SnapshotMaxAge {
				r.Funnel["universe_or_stale_snapshot"]++
				continue
			}
			if len(positions) >= o.MaxPositions {
				r.Funnel["position_limit"]++
				continue
			}
			if !universe.FinitePositive(m.MaxLeverage) {
				r.Funnel["missing_leverage_metadata"]++
				continue
			}
			leverage := math.Min(o.Leverage, m.MaxLeverage)
			side := order.setup.Side
			sign := direction(side)
			entry := c.Open * (1 + sign*(o.SlippageBPS+m.SpreadBPS/2)/10000)
			stop := order.setup.Stop
			dist := sign * (entry - stop)
			if dist <= 0 || !universe.FinitePositive(dist) {
				r.Funnel["invalid_stop_after_gap"]++
				continue
			}
			risk := balance * o.RiskPct / 100
			lossPerContract := (dist + entry*(2*o.FeeBPS+o.SlippageBPS+m.SpreadBPS/2)/10000) * m.ContractValue
			used := 0.
			for _, p := range positions {
				used += p.Margin
			}
			available := math.Max(0, balance-used)
			raw := math.Min(risk/lossPerContract, available/(entry*m.ContractValue*(1/leverage+o.FeeBPS/10000)))
			qty := math.Floor(raw/m.LotSize) * m.LotSize
			if !universe.FinitePositive(qty) || qty < m.MinSize || qty*lossPerContract > risk*(1+1e-10) {
				r.Funnel["risk_margin_or_min_size"]++
				continue
			}
			fee := qty * entry * m.ContractValue * o.FeeBPS / 10000
			positions[c.InstID] = &Trade{Symbol: c.InstID, Side: side, OpenAt: c.Start, Entry: entry, Contracts: qty, ContractValue: m.ContractValue, Stop: stop, Target: entry + sign*dist*o.RR, Margin: qty * entry * m.ContractValue / o.Leverage, Fees: fee, Risk: qty * lossPerContract}
			positions[c.InstID].Margin = qty * entry * m.ContractValue / leverage
			positions[c.InstID].SpreadBPS = m.SpreadBPS
			balance -= fee
			r.Turnover += qty * entry * m.ContractValue
			r.Funnel["opened"]++
			markEquity()
		case 1:
			c := d.Bars[ev.index]
			if prev, ok := last[c.InstID]; ok && positions[c.InstID] != nil && !prev.End.Equal(c.Start) {
				return r, fmt.Errorf("execution history gap for %s", c.InstID)
			}
			last[c.InstID] = c
			if p := positions[c.InstID]; p != nil {
				sl, tp := c.Low <= p.Stop, c.High >= p.Target
				if p.Side == models.SideSell {
					sl, tp = c.High >= p.Stop, c.Low <= p.Target
				}
				switch {
				case sl:
					price := p.Stop
					if p.Side == models.SideBuy {
						price = math.Min(price, c.Open)
					} else {
						price = math.Max(price, c.Open)
					}
					closeTrade(c.InstID, price, c.End, "stop")
				case tp:
					closeTrade(c.InstID, p.Target, c.End, "target")
				case c.End.Sub(p.OpenAt) >= o.MaxHolding:
					closeTrade(c.InstID, c.Close, c.End, "time")
				}
			}
			markEquity()
		case 3, 4:
			c := d.Bars[ev.index]
			if ev.priority == 3 {
				hs[c.InstID] = capped(hs[c.InstID], c)
			} else {
				ls[c.InstID] = capped(ls[c.InstID], c)
			}
			sig, ok := engine.OnCandle(c)
			if ev.priority != 4 {
				continue
			}
			history := hs[c.InstID]
			htfDuration, _ := time.ParseDuration(htf)
			if len(history) == 0 || c.End.Sub(history[len(history)-1].End) > htfDuration {
				if !ev.at.Before(o.Start) {
					r.Funnel["stale_htf"]++
				}
				continue
			}
			setup := Setup{}
			if o.Strategy == "smc" {
				setup, ok = smc.Evaluate(c.InstID, ls[c.InstID], hs[c.InstID], o.Shorts)
			} else if ok {
				a := ATR(ls[c.InstID], 14)
				setup = Setup{Side: sig.Side, Stop: c.Close - direction(sig.Side)*2*a, ATR: a}
			}
			if ev.at.Before(o.Start) {
				continue
			}
			if _, yes := selected[c.InstID]; !yes {
				continue
			}
			r.Funnel["analyzed_ltf"]++
			if !ok || setup.ATR <= 0 {
				r.Funnel["no_setup"]++
				continue
			}
			if setup.Side == models.SideSell && !o.Shorts {
				r.Funnel["shorts_disabled"]++
				continue
			}
			r.Funnel["signal"]++
			orders[c.InstID] = pending{setup, c.End}
		}
	}
	// Mark liquidation uses each symbol's last observed minute, never a future bar.
	symbols := []string{}
	for sym := range positions {
		symbols = append(symbols, sym)
	}
	sort.Strings(symbols)
	for _, sym := range symbols {
		c, ok := last[sym]
		if !ok || c.End.Before(positions[sym].OpenAt) {
			return r, fmt.Errorf("missing execution coverage for open position %s", sym)
		}
		closeTrade(sym, c.Close, c.End, "end_of_sample")
	}
	wins, profit, loss := 0, 0., 0.
	for _, t := range r.Trades {
		r.Net += t.Net
		if t.Net > 0 {
			wins++
			profit += t.Net
		} else {
			loss -= t.Net
		}
	}
	if len(r.Trades) > 0 {
		r.WinRate = float64(wins) / float64(len(r.Trades))
		r.Expectancy = r.Net / float64(len(r.Trades))
	}
	if loss > 0 {
		pf := profit / loss
		r.ProfitFactor = &pf
	}
	if math.Abs((balance-o.Equity)-r.Net) > 1e-7 {
		return r, fmt.Errorf("accounting invariant failed")
	}
	r.MeanDailyNet95 = dailyInterval(r.Trades, o.Start, o.End)
	if r.MeanDailyNet95 == nil {
		r.Warnings = append(r.Warnings, "Insufficient sample for daily block-bootstrap interval (requires >=30 days and >=100 closed trades); these thresholds alone do not establish an edge.")
	}
	return r, nil
}
func capped(cs []models.CandleTick, c models.CandleTick) []models.CandleTick {
	cs = append(cs, c)
	if len(cs) > 250 {
		cs = cs[len(cs)-250:]
	}
	return cs
}
