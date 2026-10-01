package research

import (
	"fmt"
	"math"
	"time"
)

type ExitLedgerEvent struct {
	At             time.Time `json:"at"`
	Kind           string    `json:"kind"`
	Reason         string    `json:"reason"`
	Price          float64   `json:"price"`
	Size           float64   `json:"size"`
	Gross          float64   `json:"gross"`
	Fee            float64   `json:"fee"`
	Funding        float64   `json:"funding"`
	RemainingAfter float64   `json:"remaining_after"`
}
type ExitOutcome struct {
	SampleID       string            `json:"sample_id"`
	Profile        ExitProfile       `json:"profile"`
	CostMultiplier float64           `json:"cost_multiplier"`
	Status         string            `json:"status"`
	CloseReason    string            `json:"close_reason"`
	CloseAt        *time.Time        `json:"close_at"`
	Gross          float64           `json:"gross"`
	Fees           float64           `json:"fees"`
	Funding        float64           `json:"funding"`
	Net            float64           `json:"net"`
	NetR           float64           `json:"net_r"`
	Remaining      float64           `json:"remaining"`
	MarkToMarket   float64           `json:"mark_to_market"`
	RemainingRisk  float64           `json:"remaining_risk"`
	Ledger         []ExitLedgerEvent `json:"ledger"`
	Warnings       []string          `json:"warnings"`
	Ambiguities    int               `json:"ambiguities"`
}

func recordExitFill(st *ExitState, ledger *[]ExitLedgerEvent, at time.Time, price, size, fee float64, reason string) error {
	if !exitPositive(price) || !exitPositive(size) || !exitFinite(fee) || size > st.Remaining+1e-9*st.LotSize || !exitNear(size/st.LotSize, math.Round(size/st.LotSize)) {
		return fmt.Errorf("invalid fill/overclose")
	}
	size = math.Min(size, st.Remaining)
	gross := exitSign(st.Side) * (price - st.Entry) * size * st.ContractValue
	if !exitFinite(gross) {
		return fmt.Errorf("gross overflow")
	}
	st.Remaining -= size
	if math.Abs(st.Remaining) < st.LotSize*1e-9 {
		st.Remaining = 0
	}
	kind := "exit"
	if st.Remaining > 0 {
		kind = "partial"
	}
	*ledger = append(*ledger, ExitLedgerEvent{At: at, Kind: kind, Reason: reason, Price: price, Size: size, Gross: gross, Fee: fee, RemainingAfter: st.Remaining})
	return nil
}
func summarizeExit(o *ExitOutcome, st ExitState, mark float64) error {
	for _, x := range o.Ledger {
		o.Gross += x.Gross
		o.Fees += x.Fee
		o.Funding += x.Funding
		if !exitFinite(o.Gross) || !exitFinite(o.Fees) || !exitFinite(o.Funding) {
			return fmt.Errorf("ledger overflow")
		}
	}
	o.Net = o.Gross + o.Fees + o.Funding
	o.NetR = o.Net / (st.InitialRiskDist * st.InitialContracts * st.ContractValue)
	o.Remaining = st.Remaining
	o.MarkToMarket = exitSign(st.Side) * (mark - st.Entry) * st.Remaining * st.ContractValue
	o.RemainingRisk = math.Max(0, exitSign(st.Side)*(mark-st.Stop)) * st.Remaining * st.ContractValue
	for _, x := range []float64{o.Net, o.NetR, o.MarkToMarket, o.RemainingRisk} {
		if !exitFinite(x) {
			return fmt.Errorf("outcome overflow")
		}
	}
	return nil
}
