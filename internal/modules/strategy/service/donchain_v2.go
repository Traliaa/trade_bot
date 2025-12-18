package service

import (
	"fmt"
	"math"
	"sync"
	"time"
	"trade_bot/internal/modules/config"

	"trade_bot/internal/models"
)

type Trend int

const (
	TrendNone Trend = iota
	TrendUp
	TrendDown
)

type DonchianV2HTF struct {
	cfg config.V2Config
	mu  sync.Mutex
	st  map[string]*v2State
}

type v2State struct {
	// LTF
	highs    []float64
	lows     []float64
	wLTF     int
	readyLTF bool

	// HTF
	emaFast  emaState
	emaSlow  emaState
	wHTF     int
	readyHTF bool
	trend    Trend

	// anti-spam: одна LTF свеча -> максимум 1 сигнал
	lastSignalEnd time.Time
}

func NewDonchianV2HTF(cfg *config.Config) *DonchianV2HTF {

	return &DonchianV2HTF{
		cfg: cfg.V2Config,
		st:  make(map[string]*v2State),
	}
}

func (e *DonchianV2HTF) get(sym string) *v2State {
	if s, ok := e.st[sym]; ok {
		return s
	}
	s := &v2State{
		highs:   make([]float64, 0, e.cfg.DonchianPeriod),
		lows:    make([]float64, 0, e.cfg.DonchianPeriod),
		emaFast: newEMA(e.cfg.HTFEmaFast),
		emaSlow: newEMA(e.cfg.HTFEmaSlow),
		trend:   TrendNone,
	}
	e.st[sym] = s
	return s
}

// OnCandle принимает закрытые свечи разных ТФ (LTF/HTF) и решает, есть ли сигнал.
// returns:
//
//	sig, ok=true  -> есть сигнал
//	becameReady=true -> по этому символу стратегия впервые "прогрелась" (LTF/HTF)
func (e *DonchianV2HTF) OnCandle(t models.CandleTick) (models.Signal, bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	tf := normTF(t.TimeframeRaw)
	st := e.get(t.InstID)

	becameReady := false

	// защита от мусора
	if t.Close <= 0 || t.High <= 0 || t.Low <= 0 {
		return models.Signal{}, false, false
	}

	switch tf {

	// ---------------- HTF: тренд ----------------
	case normTF(e.cfg.HTF):
		st.emaFast.Update(t.Close)
		st.emaSlow.Update(t.Close)
		st.wHTF++

		// готовность HTF
		if st.wHTF >= e.cfg.MinWarmupHTF && st.emaFast.Ready() && st.emaSlow.Ready() {
			if !st.readyHTF {
				st.readyHTF = true
				becameReady = true
			}

			f := st.emaFast.Value()
			s := st.emaSlow.Value()
			switch {
			case f > s:
				st.trend = TrendUp
			case f < s:
				st.trend = TrendDown
			default:
				st.trend = TrendNone
			}
		}

		return models.Signal{}, false, becameReady

	// ---------------- LTF: Donchian breakout ----------------
	case normTF(e.cfg.LTF):
		// обновляем буфер
		st.highs = append(st.highs, t.High)
		st.lows = append(st.lows, t.Low)
		if len(st.highs) > e.cfg.DonchianPeriod {
			st.highs = st.highs[1:]
			st.lows = st.lows[1:]
		}
		st.wLTF++

		if st.wLTF >= e.cfg.MinWarmupLTF && len(st.highs) >= e.cfg.DonchianPeriod {
			if !st.readyLTF {
				st.readyLTF = true
				becameReady = true
			}
		}

		// пока не готовы обе части — не сигналим
		if !st.readyLTF || !st.readyHTF || st.trend == TrendNone {
			return models.Signal{}, false, becameReady
		}

		dh := maxSlice(st.highs)
		dl := minSlice(st.lows)
		if dh <= 0 || dl <= 0 || dh <= dl {
			return models.Signal{}, false, becameReady
		}

		// ширина канала (в процентах от цены)
		chPct := (dh - dl) / t.Close
		if chPct < e.cfg.MinChannelPct {
			return models.Signal{}, false, becameReady
		}

		// импульс тела свечи
		bodyPct := math.Abs(t.Close-t.Open) / t.Close
		if bodyPct < e.cfg.MinBodyPct {
			return models.Signal{}, false, becameReady
		}

		// breakout + совпадение с HTF трендом
		var side models.Side
		if st.trend == TrendUp && t.Close > dh {
			side = models.SideBuy
		}
		if st.trend == TrendDown && t.Close < dl {
			side = models.SideSell
		}
		if side == "" {
			return models.Signal{}, false, becameReady
		}

		// антиспам: одна свеча — один сигнал максимум
		if !t.End.IsZero() && st.lastSignalEnd.Equal(t.End) {
			return models.Signal{}, false, becameReady
		}
		st.lastSignalEnd = t.End

		sig := models.Signal{
			InstID:   t.InstID,
			TF:       normTF(e.cfg.LTF),
			Side:     side,
			Price:    t.Close,
			Strategy: "donchian_v2_htf",
			Reason: fmt.Sprintf(
				"trend=%v Don[%d] chPct=%.4f bodyPct=%.4f dh=%.6f dl=%.6f emaF=%d emaS=%d",
				st.trend, e.cfg.DonchianPeriod, chPct, bodyPct, dh, dl, e.cfg.HTFEmaFast, e.cfg.HTFEmaSlow,
			),
			CreatedAt: time.Now(),
		}
		return sig, true, becameReady

	default:
		return models.Signal{}, false, false
	}
}

func (e *DonchianV2HTF) IsReady(symbol string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.st[symbol]
	if !ok {
		return false
	}
	return st.readyLTF && st.readyHTF && st.trend != TrendNone
}

func normTF(raw string) string {
	switch raw {
	case "60m", "60M", "1H", "1h":
		return "1h"
	case "15m", "15M":
		return "15m"
	case "5m", "5M":
		return "5m"
	case "10m", "10M":
		return "10m"
	default:
		return raw
	}
}

func (e *DonchianV2HTF) Name() string { return "donchian_v2_htf1h" }

func (e *DonchianV2HTF) Dump(symbol string) string {
	e.mu.Lock()
	defer e.mu.Unlock()

	st, ok := e.st[symbol]
	if !ok {
		return "v2: no state"
	}

	dh := maxSlice(st.highs)
	dl := minSlice(st.lows)

	return fmt.Sprintf(
		"v2[15m] w15=%d/%d ready15=%v dh=%.6f dl=%.6f | [1h] w1h=%d fast=%.6f slow=%.6f trend=%v ready1h=%v",
		st.wLTF, e.cfg.MinWarmupLTF, st.readyLTF, dh, dl,
		st.wHTF, st.emaFast.Value(), st.emaSlow.Value(), st.trend, st.readyHTF,
	)
}
