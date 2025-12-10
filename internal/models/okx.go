package models

import "time"

type Instrument struct {
	InstID   string `json:"instId"`
	TickSz   string `json:"tickSz"`
	LotSz    string `json:"lotSz"`
	MinSz    string `json:"minSz"`
	CtVal    string `json:"ctVal"`
	CtMult   string `json:"ctMult"`
	State    string `json:"state"`
	MaxMktSz string `json:"maxMktSz"` // макс. размер для маркет-ордера
	MaxLmtSz string `json:"maxLmtSz"` // макс. размер для лимитки (на всякий)
}

func NewInstrument(Instrument) {

}

type CandleTick struct {
	InstID string

	Open  float64
	High  float64
	Low   float64
	Close float64

	Volume       float64   // объём в контрактах (row[5])
	QuoteVolume  float64   // объём в quote (row[7]) — по желанию
	Start        time.Time // время начала свечи (ts)
	End          time.Time // время конца свечи (Start + duration)
	TimeframeRaw string    // "1m", "5m" — на всякий случай
}
