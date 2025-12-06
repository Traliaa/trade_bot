package models

import "time"

type Position struct {
	Symbol  string
	Side    string // BUY/SELL
	Qty     float64
	Entry   float64
	SL      float64
	TP      float64
	Status  string // OPEN/CLOSED
	Updated time.Time
}
