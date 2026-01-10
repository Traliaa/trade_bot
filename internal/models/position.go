package models

import "time"

// OpenPosition — приведённый вид позиций под интерфейс бота
// (значения мапятся из формата OKX /api/v5/account/positions).
type OpenPosition struct {
	Symbol       string  // instId
	PositionType int     // 1 = long, 2 = short
	HoldVol      float64 // pos
	HoldAvgPrice float64 // avgPx
	Leverage     int     // lever
	Realised     float64 // пока не заполняем (0)

	Size             float64
	EntryPrice       float64
	LastPrice        float64
	UnrealizedPnl    float64
	UnrealizedPnlPct float64
	Side             string

	Qty     float64
	Entry   float64
	SL      float64
	TP      float64
	Status  string // OPEN/CLOSED
	Updated time.Time
}

type PositionTrailState struct {
	InstID  string
	PosSide string // "long"/"short"

	Entry    float64
	SL       float64
	TP       float64
	RiskDist float64
	TickSz   float64

	AlgoID string
	Size   float64

	MFE float64 // long: max price; short: min price

	MovedToBE    bool
	LockedProfit bool

	LastTrailEnd time.Time
	LastTrailAt  time.Time
}

func (st *PositionTrailState) UpdateMFE(high, low float64) {
	if st.PosSide == "long" {
		if high > st.MFE {
			st.MFE = high
		}
		return
	}

	// short
	if st.MFE == 0 || low < st.MFE {
		st.MFE = low
	}
}

func (st *PositionTrailState) MaybeTrailOnClosedCandle(
	high float64,
	low float64,
	end time.Time,
) (newSL float64, ok bool) {
	// защита: 1 апдейт на свечу
	if !st.LastTrailEnd.IsZero() && st.LastTrailEnd.Equal(end) {
		return 0, false
	}

	// обновляем MFE по закрытой свече
	st.UpdateMFE(high, low)

	R := st.RiskDist
	if R <= 0 {
		return 0, false
	}

	// helpers
	improve := func(candidate float64) bool {
		if st.PosSide == "long" {
			return candidate > st.SL
		}
		return candidate < st.SL
	}

	// 1) BE+ после 1R
	if !st.MovedToBE {
		if st.PosSide == "long" && st.MFE >= st.Entry+1.0*R {
			cand := st.Entry + 0.05*R
			if improve(cand) {
				st.MovedToBE = true
				st.LastTrailEnd = end
				st.LastTrailAt = end
				return cand, true
			}
		}
		if st.PosSide == "short" && st.MFE <= st.Entry-1.0*R {
			cand := st.Entry - 0.05*R
			if improve(cand) {
				st.MovedToBE = true
				st.LastTrailEnd = end
				st.LastTrailAt = end
				return cand, true
			}
		}
	}

	// 2) “почти TP”: 80% пути -> SL в +0.5R
	if !st.LockedProfit && st.TP > 0 {
		var progress float64
		if st.PosSide == "long" {
			denom := (st.TP - st.Entry)
			if denom > 0 {
				progress = (st.MFE - st.Entry) / denom
			}
		} else {
			denom := (st.Entry - st.TP)
			if denom > 0 {
				progress = (st.Entry - st.MFE) / denom
			}
		}

		if progress >= 0.80 {
			var cand float64
			if st.PosSide == "long" {
				cand = st.Entry + 0.5*R
			} else {
				cand = st.Entry - 0.5*R
			}
			if improve(cand) {
				st.LockedProfit = true
				st.LastTrailEnd = end
				st.LastTrailAt = end
				return cand, true
			}
		}
	}

	return 0, false
}
