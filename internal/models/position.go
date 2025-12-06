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
