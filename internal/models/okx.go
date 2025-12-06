package models

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
