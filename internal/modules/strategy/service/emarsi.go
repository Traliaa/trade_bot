package service

import (
	"fmt"
	"sync"
	"trade_bot/internal/models"
)

type EMARSI struct {
	mu sync.Mutex

	cfg *models.TradingSettings

	emaShort map[string]float64
	emaLong  map[string]float64
	rsi      map[string]*rsiState

	samples  map[string]int    // сколько тиков/свечей обработано
	lastSide map[string]string // последний сгенерённый сигнал BUY/SELL
}

type rsiState struct {
	prev        float64
	avgGain     float64
	avgLoss     float64
	initialized bool
}

func NewEMARSI(cfg *models.TradingSettings) *EMARSI {
	return &EMARSI{
		cfg:      cfg,
		emaShort: map[string]float64{},
		emaLong:  map[string]float64{},
		rsi:      map[string]*rsiState{},
		samples:  map[string]int{},
		lastSide: map[string]string{},
	}
}

func (e *EMARSI) OnCandle(symbol string, c models.CandleTick) models.Signal {
	// старая логика принимала только цену Close
	side, ok := e.Update(
		symbol,
		c.Close,
		e.cfg.EMAShort,
		e.cfg.EMALong,
		e.cfg.RSIPeriod,
		e.cfg.RSIOverbought,
		e.cfg.RSIOSold,
	)
	if !ok {
		return models.Signal{InstID: symbol, Side: models.SideNone}
	}

	var s models.Side
	if side == "BUY" {
		s = models.SideBuy
	} else if side == "SELL" {
		s = models.SideSell
	}

	return models.Signal{
		InstID: symbol,
		Side:   s,
		Price:  c.Close,
		Reason: fmt.Sprintf("EMA/RSI signal %s @ %.5f", s, c.Close),
	}
}

func (s *EMARSI) Update(symbol string, price float64,
	emaShortN, emaLongN, rsiN int, ob, os float64) (string, bool) {

	s.mu.Lock()
	defer s.mu.Unlock()

	// EMA
	kShort := 2.0 / float64(emaShortN+1)
	kLong := 2.0 / float64(emaLongN+1)
	s.emaShort[symbol] = s.emaShort[symbol] + kShort*(price-s.emaShort[symbol])
	s.emaLong[symbol] = s.emaLong[symbol] + kLong*(price-s.emaLong[symbol])

	// RSI
	st := s.rsi[symbol]
	if st == nil {
		st = &rsiState{}
		s.rsi[symbol] = st
	}
	if !st.initialized {
		st.prev = price
		st.initialized = true
		return "", false
	}

	change := price - st.prev
	gain, loss := 0.0, 0.0
	if change > 0 {
		gain = change
	} else {
		loss = -change
	}

	alpha := 1.0 / float64(rsiN)
	if st.avgGain == 0 && st.avgLoss == 0 {
		st.avgGain, st.avgLoss = gain, loss
	} else {
		st.avgGain = (1-alpha)*st.avgGain + alpha*gain
		st.avgLoss = (1-alpha)*st.avgLoss + alpha*loss
	}
	st.prev = price

	rs := 0.0
	if st.avgLoss != 0 {
		rs = st.avgGain / st.avgLoss
	}
	rsi := 100 - (100 / (1 + rs))

	// прогрев: ждём достаточно точек
	s.samples[symbol]++
	warmup := emaLongN
	if rsiN+1 > warmup {
		warmup = rsiN + 1
	}
	if s.samples[symbol] < warmup {
		return "", false
	}

	// «идеальный» сигнал по текущему состоянию
	side := ""
	if s.emaShort[symbol] > s.emaLong[symbol] && rsi < os {
		side = "BUY"
	} else if s.emaShort[symbol] < s.emaLong[symbol] && rsi > ob {
		side = "SELL"
	}
	if side == "" {
		return "", false
	}

	// один сигнал на смену стороны
	if side == s.lastSide[symbol] {
		return "", false
	}
	s.lastSide[symbol] = side
	return side, true
}

func (s *EMARSI) Dump(symbol string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fmt.Sprintf("EMA_S=%.4f EMA_L=%.4f", s.emaShort[symbol], s.emaLong[symbol])
}
