package strategy

import (
	"fmt"
	"sync"
)

type EMARSI struct {
	mu       sync.Mutex
	emaShort map[string]float64
	emaLong  map[string]float64
	rsi      map[string]*rsiState
}

type rsiState struct {
	prev        float64
	avgGain     float64
	avgLoss     float64
	initialized bool
}

func NewEMARSI() *EMARSI {
	return &EMARSI{emaShort: map[string]float64{}, emaLong: map[string]float64{}, rsi: map[string]*rsiState{}}
}

func (s *EMARSI) Update(symbol string, price float64, emaShortN, emaLongN, rsiN int, ob, os float64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kShort := 2.0 / float64(emaShortN+1)
	kLong := 2.0 / float64(emaLongN+1)
	s.emaShort[symbol] = s.emaShort[symbol] + kShort*(price-s.emaShort[symbol])
	s.emaLong[symbol] = s.emaLong[symbol] + kLong*(price-s.emaLong[symbol])

	r := s.rsi[symbol]
	if r == nil {
		r = &rsiState{}
		s.rsi[symbol] = r
	}
	if !r.initialized {
		r.prev = price
		r.initialized = true
		return "", false
	}
	change := price - r.prev
	gain := 0.0
	loss := 0.0
	if change > 0 {
		gain = change
	} else {
		loss = -change
	}
	alpha := 1.0 / float64(rsiN)
	if r.avgGain == 0 && r.avgLoss == 0 {
		r.avgGain, r.avgLoss = gain, loss
	} else {
		r.avgGain = (1-alpha)*r.avgGain + alpha*gain
		r.avgLoss = (1-alpha)*r.avgLoss + alpha*loss
	}
	r.prev = price
	rs := 0.0
	if r.avgLoss != 0 {
		rs = r.avgGain / r.avgLoss
	}
	rsi := 100 - (100 / (1 + rs))

	if s.emaShort[symbol] > s.emaLong[symbol] && rsi < os {
		return "BUY", true
	}
	if s.emaShort[symbol] < s.emaLong[symbol] && rsi > ob {
		return "SELL", true
	}
	return "", false
}

func (s *EMARSI) Dump(symbol string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("EMA_S=%.4f EMA_L=%.4f", s.emaShort[symbol], s.emaLong[symbol])
}
