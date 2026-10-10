//go:build linux || darwin

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"
)

type MetadataObservation struct {
	Status             string    `json:"status"`
	Reason             string    `json:"reason"`
	Symbol             string    `json:"symbol"`
	Source             string    `json:"source"`
	ObservedAt         time.Time `json:"observed_at"`
	HistoryVerified    bool      `json:"history_verified"`
	TickSize           string    `json:"tick_size"`
	LotSize            string    `json:"lot_size"`
	MinSize            string    `json:"min_size"`
	ContractValue      string    `json:"contract_value"`
	ContractMultiplier string    `json:"contract_multiplier"`
	ValueCurrency      string    `json:"value_currency"`
	SettleCurrency     string    `json:"settle_currency"`
	ContractType       string    `json:"contract_type"`
	State              string    `json:"state"`
}

func (c *ArchiveClient) CollectMetadata(ctx context.Context, symbol string) (MetadataObservation, error) {
	m := MetadataObservation{Status: "pending", Source: "okx_public_instruments"}
	fail := func(code string) (MetadataObservation, error) { m.Reason = code; return m, errors.New(code) }
	if c == nil || c.now == nil || !usdtSwap.MatchString(symbol) {
		return fail("market_metadata_scope_invalid")
	}
	m.Symbol = symbol
	m.Status = "incomplete"
	b, err := c.Get(ctx, "/api/v5/public/instruments", url.Values{"instId": {symbol}, "instType": {"SWAP"}})
	if err != nil {
		return fail(err.Error())
	}
	if ctx.Err() != nil {
		return fail("archive_cancelled")
	}
	rows, err := marketRows(b)
	if err != nil || len(rows) != 1 {
		return fail("market_metadata_missing")
	}
	var r struct {
		Symbol   string `json:"instId"`
		Type     string `json:"instType"`
		CtType   string `json:"ctType"`
		Settle   string `json:"settleCcy"`
		Currency string `json:"ctValCcy"`
		Value    string `json:"ctVal"`
		Mult     string `json:"ctMult"`
		Tick     string `json:"tickSz"`
		Lot      string `json:"lotSz"`
		Min      string `json:"minSz"`
		State    string `json:"state"`
	}
	if !exactPrivateObject(rows[0], []string{"instId", "instType", "ctType", "settleCcy", "ctValCcy", "ctVal", "ctMult", "tickSz", "lotSz", "minSz", "state"}, true) || json.Unmarshal(rows[0], &r) != nil || r.Symbol != symbol || r.Type != "SWAP" || r.CtType != "linear" || r.Settle != "USDT" || !feeCurrency.MatchString(r.Currency) {
		return fail("market_metadata_invalid")
	}
	for _, v := range []string{r.Value, r.Mult, r.Tick, r.Lot, r.Min} {
		n, ok := decimal(v)
		if !ok || n.Sign() <= 0 {
			return fail("market_metadata_invalid")
		}
	}
	switch r.State {
	case "live", "suspend", "preopen", "test":
	default:
		return fail("market_metadata_state_unknown")
	}
	m.Status = "complete"
	m.ObservedAt = c.now().UTC()
	m.TickSize = r.Tick
	m.LotSize = r.Lot
	m.MinSize = r.Min
	m.ContractValue = r.Value
	m.ContractMultiplier = r.Mult
	m.ValueCurrency = r.Currency
	m.SettleCurrency = r.Settle
	m.ContractType = r.CtType
	m.State = r.State
	return m, nil
}
