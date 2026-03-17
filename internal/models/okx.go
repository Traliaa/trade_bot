package models

import "time"

type ContractKind int

const (
	ContractUnknown ContractKind = iota
	ContractLinearUSDT
	ContractInverseCoin
)

type Instrument struct {
	InstID    string
	Kind      ContractKind
	SettleCcy string
	CtValCcy  string

	LastPx   float64
	LotSz    float64
	MinSz    float64
	TickSz   float64
	CtVal    float64 // effective (ctVal * ctMult)
	MaxMktSz float64 // optional, 0 если неизвестно
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

// TradeParams содержит все рассчитанные параметры сделки.
type TradeParams struct {
	Entry     float64
	SL        float64
	TP        float64
	Size      float64
	TickSize  float64
	RiskPct   float64
	RR        float64
	RiskDist  float64
	Leverage  int
	Direction string // "BUY" или "SELL"
}

type PositionInfo struct {
	Symbol           string
	Side             string // LONG/SHORT
	Size             float64
	EntryPrice       float64
	LastPrice        float64
	UnrealizedPnl    float64
	UnrealizedPnlPct float64 // %
	Leverage         int
}

type TransactionDetailsResponse struct {
	Code string                    `json:"code"`
	Msg  string                    `json:"msg"`
	Data []TransactionDetailRecord `json:"data"`
}

type TransactionDetailRecord struct {
	InstID   string `json:"instId"`
	PosSide  string `json:"posSide"`
	Side     string `json:"side"`
	OrdID    string `json:"ordId"`
	TradeID  string `json:"tradeId"`
	FillPx   string `json:"fillPx"`
	FillSz   string `json:"fillSz"`
	FillTime string `json:"fillTime"`
	FillPnl  string `json:"fillPnl"`
	ExecType string `json:"execType"`
}
type TradeFill struct {
	InstID      string    `json:"inst_id"`
	PosSide     string    `json:"pos_side"`
	Side        string    `json:"side"`
	FillPx      float64   `json:"fill_px"`
	FillSz      float64   `json:"fill_sz"`
	Fee         float64   `json:"fee"`
	RealizedPnL float64   `json:"realized_pnl"`
	TradeID     string    `json:"trade_id"`
	FillTime    time.Time `json:"fill_time"`
}
