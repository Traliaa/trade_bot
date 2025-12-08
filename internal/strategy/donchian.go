package strategy

import (
	"fmt"
	"math"
	"sync"
)

// Side как у тебя в раннере: "BUY"/"SELL" или пустая строка.
type Side string

const (
	SideNone Side = ""
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// Candle — подгони под свой тип, если он уже есть.
type Candle struct {
	Open, High, Low, Close float64
}

// Signal — простой ответ стратегии.
type Signal struct {
	Symbol string
	Side   Side // BUY / SELL / ""
	Price  float64
	Reason string
}

// DonchianConfig — параметры стратегии.
type DonchianConfig struct {
	Period    int // N свечей, например 20
	TrendEma  int // EMA-фильтр, например 50
	MinWarmup int // сколько свечей ждать до сигналов; можно = max(Period, TrendEma)
}

// Donchian — стратегия пробоя канала Дончиана с EMA-фильтром.
type Donchian struct {
	cfg DonchianConfig

	mu    sync.Mutex
	state map[string]*symbolState
}

type symbolState struct {
	highs []float64
	lows  []float64
	ema   emaState

	lastSignal Side
}

type emaState struct {
	period int
	alpha  float64
	value  float64
	warmup int
}

func newEMA(period int) emaState {
	if period <= 1 {
		period = 1
	}
	return emaState{
		period: period,
		alpha:  2.0 / (float64(period) + 1),
	}
}

func (e *emaState) Update(price float64) {
	if e.warmup == 0 {
		e.value = price
		e.warmup = 1
		return
	}
	e.value = e.alpha*price + (1-e.alpha)*e.value
	if e.warmup < e.period {
		e.warmup++
	}
}

func (e *emaState) Ready() bool {
	return e.warmup >= e.period
}

func (e *emaState) Value() float64 { return e.value }

func NewDonchian(cfg DonchianConfig) *Donchian {
	if cfg.Period <= 0 {
		cfg.Period = 20
	}
	if cfg.TrendEma <= 0 {
		cfg.TrendEma = 50
	}
	if cfg.MinWarmup <= 0 {
		cfg.MinWarmup = int(math.Max(float64(cfg.Period), float64(cfg.TrendEma)))
	}
	return &Donchian{
		cfg:   cfg,
		state: make(map[string]*symbolState),
	}
}

func (s *Donchian) get(symbol string) *symbolState {
	if st, ok := s.state[symbol]; ok {
		return st
	}
	st := &symbolState{
		highs: make([]float64, 0, s.cfg.Period),
		lows:  make([]float64, 0, s.cfg.Period),
		ema:   newEMA(s.cfg.TrendEma),
	}
	s.state[symbol] = st
	return st
}

// OnCandle — вызываешь на закрытии каждой свечи.
// Возвращает Signal с Side=BUY/SELL либо SideNone.
func (s *Donchian) OnCandle(symbol string, c Candle) Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.get(symbol)

	// обновляем EMA тренда по закрытию
	st.ema.Update(c.Close)

	// добавляем high/low в буфер
	st.highs = append(st.highs, c.High)
	st.lows = append(st.lows, c.Low)
	if len(st.highs) > s.cfg.Period {
		st.highs = st.highs[1:]
		st.lows = st.lows[1:]
	}

	// ещё не достаточно данных
	if len(st.highs) < s.cfg.Period || !st.ema.Ready() {
		return Signal{Symbol: symbol, Side: SideNone}
	}

	// считаем Donchian high/low
	dh := maxSlice(st.highs)
	dl := minSlice(st.lows)
	ema := st.ema.Value()

	// фильтр тренда: торгуем только в сторону EMA
	var side Side
	var reason string

	// пробой вверх: close выше канала и выше EMA
	if c.Close > dh && c.Close > ema {
		side = SideBuy
		reason = fmt.Sprintf("Donchian breakout UP: close=%.5f > dh=%.5f & ema=%.5f", c.Close, dh, ema)
	}

	// пробой вниз: close ниже канала и ниже EMA
	if c.Close < dl && c.Close < ema {
		side = SideSell
		reason = fmt.Sprintf("Donchian breakout DOWN: close=%.5f < dl=%.5f & ema=%.5f", c.Close, dl, ema)
	}

	if side == SideNone {
		return Signal{Symbol: symbol, Side: SideNone}
	}

	st.lastSignal = side

	return Signal{
		Symbol: symbol,
		Side:   side,
		Price:  c.Close,
		Reason: reason,
	}
}

// Dump — чтобы в логах показывать состояние (как ты делал с EMARSI).
func (s *Donchian) Dump(symbol string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.state[symbol]
	if !ok || len(st.highs) == 0 {
		return "Donchian: warmup"
	}

	dh := maxSlice(st.highs)
	dl := minSlice(st.lows)
	return fmt.Sprintf("Donchian[period=%d] H=%.5f L=%.5f EMA=%d=%.5f last=%s",
		s.cfg.Period, dh, dl, s.cfg.TrendEma, st.ema.Value(), st.lastSignal)
}

// вспомогательные
func maxSlice(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, v := range xs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func minSlice(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, v := range xs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}
