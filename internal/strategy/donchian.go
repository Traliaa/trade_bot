package strategy

import (
	"fmt"
	"math"
	"sync"
	"trade_bot/internal/models"
)

// DonchianConfig — параметры стратегии.
type DonchianConfig struct {
	Period        int     // N свечей, например 20
	TrendEma      int     // EMA-фильтр, например 50
	MinWarmup     int     // >= max(Period, TrendEma)
	MinChannelPct float64 // минимальная высота канала, напр. 0.007 (0.7%)
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

	lastClose  float64
	lastSignal models.Side
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
	if cfg.MinChannelPct <= 0 {
		cfg.MinChannelPct = 0.007 // 0.7% по умолчанию
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
// ВАЖНО: пробой считаем по предыдущим N свечам, потом только обновляем окно.
func (s *Donchian) OnCandle(symbol string, c models.CandleTick) models.Signal {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.get(symbol)

	// 1) обновляем EMA тренда по close
	st.ema.Update(c.Close)

	// 2) добавляем high/low
	st.highs = append(st.highs, c.High)
	st.lows = append(st.lows, c.Low)
	if len(st.highs) > s.cfg.Period {
		st.highs = st.highs[1:]
		st.lows = st.lows[1:]
	}

	if len(st.highs) < s.cfg.Period || !st.ema.Ready() {
		st.lastClose = c.Close
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}

	dh := maxSlice(st.highs)
	dl := minSlice(st.lows)
	ema := st.ema.Value()

	// --- 3) фильтр по высоте канала
	channelHeight := dh - dl
	if channelHeight <= 0 {
		st.lastClose = c.Close
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}
	channelPct := channelHeight / c.Close
	if channelPct < s.cfg.MinChannelPct {
		// канал узкий, шум — не торгуем
		st.lastClose = c.Close
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}

	// --- 4) EMA-тренд
	// (очень простой фильтр: EMA сейчас выше/ниже цены, можно усложнить,
	// если будешь хранить прошлое значение EMA)
	var trendUp, trendDown bool
	if ema > (dl+dh)/2 {
		trendUp = true
	}
	if ema < (dl+dh)/2 {
		trendDown = true
	}

	// --- 5) паттерн "предыдущая внутри, текущая снаружи"
	prevClose := st.lastClose
	st.lastClose = c.Close // обновляем на будущее

	// предыдущая была внутри канала?
	prevInside := prevClose >= dl && prevClose <= dh
	// текущая закрылась выше/ниже канала?
	nowAbove := c.Close > dh
	nowBelow := c.Close < dl

	var side models.Side
	var reason string

	// Лонг: предыдущая внутри, сейчас пробили вверх, тренд вверх
	if prevInside && nowAbove && trendUp {
		side = models.SideBuy
		reason = fmt.Sprintf(
			"Donchian breakout UP: prevIn[%.5f] dh=%.5f dl=%.5f close=%.5f ema=%.5f ch=%.4f%%",
			prevClose, dh, dl, c.Close, ema, channelPct*100,
		)
	}

	// Шорт: предыдущая внутри, сейчас пробили вниз, тренд вниз
	if prevInside && nowBelow && trendDown {
		side = models.SideSell
		reason = fmt.Sprintf(
			"Donchian breakout DOWN: prevIn[%.5f] dh=%.5f dl=%.5f close=%.5f ema=%.5f ch=%.4f%%",
			prevClose, dh, dl, c.Close, ema, channelPct*100,
		)
	}

	if side == models.SideNone {
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}

	st.lastSignal = side

	return models.Signal{
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
