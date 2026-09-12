// Package research is offline-only. Nothing here can submit an exchange order.
package research

import (
	"math"
	"time"
	"trade_bot/internal/models"
)

type Setup struct {
	Side models.Side
	Stop float64
	ATR  float64
}
type sweepState struct {
	side                models.Side
	extreme, level, atr float64
	phase, bars         int
}
type SMC struct {
	pending map[string]sweepState
	last    map[string]time.Time
}

func NewSMC() *SMC { return &SMC{pending: map[string]sweepState{}, last: map[string]time.Time{}} }
func ATR(cs []models.CandleTick, n int) float64 {
	if len(cs) < n+1 {
		return 0
	}
	sum := 0.
	for i := len(cs) - n; i < len(cs); i++ {
		c, p := cs[i], cs[i-1]
		sum += math.Max(c.High-c.Low, math.Max(math.Abs(c.High-p.Close), math.Abs(c.Low-p.Close)))
	}
	return sum / float64(n)
}

// Evaluate consumes a newly CLOSED LTF bar. A sweep, BOS and retest must occur
// on separate bars; extrema are rolling past values, never future-confirmed pivots.
func (s *SMC) Evaluate(symbol string, ltf, htf []models.CandleTick, shorts bool) (Setup, bool) {
	if len(ltf) < 21 || len(htf) < 20 {
		return Setup{}, false
	}
	c := ltf[len(ltf)-1]
	if !c.End.After(s.last[symbol]) {
		return Setup{}, false
	}
	s.last[symbol] = c.End
	prior := ltf[:len(ltf)-1]
	atr := ATR(prior, 14)
	if atr <= 0 {
		return Setup{}, false
	}
	h := htf[len(htf)-1]
	if h.End.After(c.End) {
		return Setup{}, false
	}
	mean := 0.
	for _, v := range htf[len(htf)-20:] {
		mean += v.Close / 20
	}
	bull, bear := h.Close > mean, h.Close < mean
	if p, ok := s.pending[symbol]; ok {
		p.bars++
		bad := p.bars > 6 || (p.side == models.SideBuy && (c.Low < p.extreme || !bull)) || (p.side == models.SideSell && (c.High > p.extreme || !bear || !shorts))
		if bad {
			delete(s.pending, symbol)
			return Setup{}, false
		}
		if p.phase == 1 {
			displacement := math.Abs(c.Close-c.Open) >= .8*p.atr
			if displacement && ((p.side == models.SideBuy && c.Close > p.level && c.Close > c.Open) || (p.side == models.SideSell && c.Close < p.level && c.Close < c.Open)) {
				p.phase = 2
			}
			s.pending[symbol] = p
			return Setup{}, false
		}
		retest := p.side == models.SideBuy && c.Low <= p.level+.2*p.atr && c.Close > p.level && c.Close > c.Open
		retest = retest || (p.side == models.SideSell && c.High >= p.level-.2*p.atr && c.Close < p.level && c.Close < c.Open)
		if retest {
			delete(s.pending, symbol)
			stop := p.extreme - .1*p.atr
			if p.side == models.SideSell {
				stop = p.extreme + .1*p.atr
			}
			return Setup{Side: p.side, Stop: stop, ATR: p.atr}, true
		}
		s.pending[symbol] = p
		return Setup{}, false
	}
	low, high := math.Inf(1), math.Inf(-1)
	bosHigh, bosLow := math.Inf(-1), math.Inf(1)
	for _, v := range prior[len(prior)-20:] {
		low = math.Min(low, v.Low)
		high = math.Max(high, v.High)
	}
	for _, v := range prior[len(prior)-5:] {
		bosHigh = math.Max(bosHigh, v.High)
		bosLow = math.Min(bosLow, v.Low)
	}
	if bull && c.Low < low-.1*atr && c.Close > low {
		s.pending[symbol] = sweepState{side: models.SideBuy, extreme: c.Low, level: bosHigh, atr: atr, phase: 1}
	}
	if shorts && bear && c.High > high+.1*atr && c.Close < high {
		s.pending[symbol] = sweepState{side: models.SideSell, extreme: c.High, level: bosLow, atr: atr, phase: 1}
	}
	return Setup{}, false
}
