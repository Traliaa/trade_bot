package strategy

import (
	"fmt"
	"log"
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
	cfg.Period = 20
	cfg.TrendEma = 50
	cfg.MinWarmup = int(math.Max(float64(cfg.Period), float64(cfg.TrendEma)))
	cfg.MinChannelPct = 0.003
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

	// 0) prevClose нужен для "prev inside"
	prevClose := st.lastClose
	st.lastClose = c.Close

	// EMA по текущему close (можно так)
	st.ema.Update(c.Close)

	// 1) Если данных меньше Period — просто накапливаем и выходим
	if len(st.highs) < s.cfg.Period {
		st.highs = append(st.highs, c.High)
		st.lows = append(st.lows, c.Low)
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}

	// 2) ВАЖНО: считаем канал по ПРЕДЫДУЩИМ свечам (st.highs/st.lows),
	// ДО того как добавили текущую свечу
	dh := maxSlice(st.highs)
	dl := minSlice(st.lows)
	ema := st.ema.Value()

	// 3) Фильтр warmup для prevClose
	if prevClose == 0 || !st.ema.Ready() {
		// теперь обновляем буфер и уходим
		st.highs = append(st.highs[1:], c.High)
		st.lows = append(st.lows[1:], c.Low)
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}

	// 4) (если ты добавил MinChannelPct) — проверь, не задушил ли ты всё
	channelPct := (dh - dl) / c.Close
	if s.cfg.MinChannelPct > 0 && channelPct < s.cfg.MinChannelPct {
		st.highs = append(st.highs[1:], c.High)
		st.lows = append(st.lows[1:], c.Low)
		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}

	// 5) "prev inside" / "now outside"
	prevInside := prevClose >= dl && prevClose <= dh
	nowAbove := c.Close > dh
	nowBelow := c.Close < dl

	var side models.Side
	var reason string

	if prevInside && nowAbove && c.Close > ema {
		side = models.SideBuy
		reason = fmt.Sprintf("Donchian UP prevInside close=%.6f > dh=%.6f ema=%.6f", c.Close, dh, ema)
	}
	if prevInside && nowBelow && c.Close < ema {
		side = models.SideSell
		reason = fmt.Sprintf("Donchian DOWN prevInside close=%.6f < dl=%.6f ema=%.6f", c.Close, dl, ema)
	}

	// 6) Сдвигаем окно (добавляем текущую свечу)
	st.highs = append(st.highs[1:], c.High)
	st.lows = append(st.lows[1:], c.Low)

	if side == models.SideNone {
		log.Printf("[DON-DBG] %s close=%.6f dh=%.6f dl=%.6f prev=%.6f prevInside=%v nowAbove=%v nowBelow=%v emaReady=%v chPct=%.4f",
			symbol, c.Close, dh, dl, prevClose, prevInside, nowAbove, nowBelow, st.ema.Ready(), channelPct*100)

		return models.Signal{Symbol: symbol, Side: models.SideNone}
	}
	st.lastSignal = side
	log.Printf("[DON-SIG] %s %s %s", symbol, side, reason)
	return models.Signal{Symbol: symbol, Side: side, Price: c.Close, Reason: reason}
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
