package service

import (
	"fmt"
	"math"
	"strings"
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
		// 0) если буфер уже прогрет — считаем канал ДО добавления текущей свечи
		var (
			dh, dl  float64
			haveCh  bool
			chPct   float64
			bodyPct float64
		)

		if len(st.highs) >= e.cfg.DonchianPeriod {
			dh = maxSlice(st.highs)
			dl = minSlice(st.lows)
			if dh > 0 && dl > 0 && dh > dl {
				haveCh = true
			}
		}

		// 1) инкремент прогрева LTF (по закрытым свечам)
		st.wLTF++
		if st.wLTF >= e.cfg.MinWarmupLTF && len(st.highs) >= e.cfg.DonchianPeriod && st.readyLTF == false {
			st.readyLTF = true
			becameReady = true
		}

		// 2) пробуем сформировать сигнал (только если уже готовы HTF+LTF и канал есть)
		if haveCh && st.readyLTF && st.readyHTF && st.trend != TrendNone {
			// ширина канала лучше считать от середины/цены, но оставим close
			if t.Close > 0 {
				chPct = (dh - dl) / t.Close
			}
			if chPct >= e.cfg.MinChannelPct {
				bodyPct = math.Abs(t.Close-t.Open) / t.Close
				if bodyPct >= e.cfg.MinBodyPct {

					// breakout buffer (антишум)
					bo := e.cfg.BreakoutPct
					if bo < 0 {
						bo = 0
					}

					// верх/низ с буфером
					upLevel := dh * (1 + bo)
					dnLevel := dl * (1 - bo)

					// breakout + совпадение с HTF трендом
					var side models.Side
					if st.trend == TrendUp && t.Close > upLevel {
						side = models.SideBuy
					}
					if st.trend == TrendDown && t.Close < dnLevel {
						side = models.SideSell
					}
					if side == "" {
						return models.Signal{}, false, becameReady
					}

					// антиспам: одна и та же LTF свеча -> 1 сигнал
					if t.End.IsZero() || !st.lastSignalEnd.Equal(t.End) {
						st.lastSignalEnd = t.End

						sig := models.Signal{
							InstID:   t.InstID,
							TF:       normTF(e.cfg.LTF),
							Side:     side,
							Price:    t.Close,
							Strategy: "donchian_v2_htf",
							Reason: fmt.Sprintf(
								"trend=%v Don[%d] chPct=%.4f bodyPct=%.4f dh=%.6f dl=%.6f bo=%.4f up=%.6f dn=%.6f",
								st.trend, e.cfg.DonchianPeriod, chPct, bodyPct, dh, dl,
								bo, upLevel, dnLevel,
							),
							CreatedAt: time.Now(),
						}

						// 3) теперь добавляем текущую свечу в буфер и выходим с сигналом
						st.highs = append(st.highs, t.High)
						st.lows = append(st.lows, t.Low)
						if len(st.highs) > e.cfg.DonchianPeriod {
							st.highs = st.highs[1:]
							st.lows = st.lows[1:]
						}

						fmt.Printf("[SIG] %s %s close=%.6f dh=%.6f dl=%.6f trend=%v\n",
							t.InstID, side, t.Close, dh, dl, st.trend)
						return sig, true, becameReady
					}

				}
			}
		}

		// 4) если сигнала нет — просто обновляем буфер текущей свечой
		st.highs = append(st.highs, t.High)
		st.lows = append(st.lows, t.Low)
		if len(st.highs) > e.cfg.DonchianPeriod {
			st.highs = st.highs[1:]
			st.lows = st.lows[1:]
		}

		return models.Signal{}, false, becameReady

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
	s := strings.TrimSpace(strings.ToLower(raw))
	s = strings.TrimPrefix(s, "candle")
	switch s {
	case "60m", "1h":
		return "1h"
	case "15m":
		return "15m"
	case "5m":
		return "5m"
	case "10m":
		return "10m"
	default:
		return s
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
func (t Trend) String() string {
	switch t {
	case TrendUp:
		return "up"
	case TrendDown:
		return "down"
	default:
		return "none"
	}
}
