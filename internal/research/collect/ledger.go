package collect

import (
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Fill and LedgerInput are private working data, never bundle payloads.
type Fill struct {
	ID, OrderID, Symbol, Side, PosSide string
	At                                 time.Time
	Size, Price, Fee, PnL, FeeCurrency string
}

func (Fill) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private_evidence_not_serializable")
}

type FundingEvidence struct {
	Amount, Currency string
	Complete         bool
}
type LedgerInput struct {
	Symbol, Side                string
	EntryAt, Cutoff             time.Time
	EntrySize                   string
	EntryOrderIDs, ExitOrderIDs []string
	Fills                       []Fill
	ArchiveComplete             bool
	DBState                     string
	ExchangeRemaining           *string
	RecordedTradingNet          *string          // excludes funding; never compare different scopes
	Funding                     *FundingEvidence // caller must prove attribution and complete coverage
}

func (LedgerInput) MarshalJSON() ([]byte, error) {
	return nil, errors.New("private_evidence_not_serializable")
}

type Execution struct {
	Sequence int       `json:"sequence"`
	Role     string    `json:"role"`
	At       time.Time `json:"at"`
	Size     string    `json:"size"`
	Price    string    `json:"price"`
	Fee      *string   `json:"fee_usdt"`
	PnL      string    `json:"realized_pnl_usdt"`
}
type LedgerReport struct {
	Reconciliation string      `json:"reconciliation"`
	Costs          string      `json:"costs"`
	Reasons        []string    `json:"reasons"`
	Closed         bool        `json:"closed"`
	FillCount      int         `json:"fill_count"`
	EntrySize      string      `json:"observed_entry_size"`
	ExitSize       string      `json:"observed_exit_size"`
	Gross          string      `json:"observed_gross_usdt"`
	Fees           string      `json:"observed_fees_usdt"`
	TradingNet     *string     `json:"reconciled_trading_net_usdt"`
	Net            *string     `json:"reconciled_net_with_funding_usdt"`
	Executions     []Execution `json:"executions"`
}

var plainDecimal = regexp.MustCompile(`^-?[0-9]{1,32}(\.[0-9]{1,32})?$`)
var usdtSwap = regexp.MustCompile(`^[A-Z0-9]{1,24}-USDT-SWAP$`)

func decimal(raw string) (*big.Rat, bool) {
	if len(raw) > 66 || !plainDecimal.MatchString(raw) {
		return nil, false
	}
	v, ok := new(big.Rat).SetString(raw)
	return v, ok
}
func decimalText(v *big.Rat) string {
	s := strings.TrimRight(strings.TrimRight(v.FloatString(32), "0"), ".")
	if s == "-0" {
		return "0"
	}
	return s
}
func sameDecimal(a, b string) bool {
	x, ok := decimal(a)
	y, yes := decimal(b)
	return ok && yes && x.Cmp(y) == 0
}
func sameFill(a, b Fill) bool {
	return a.OrderID == b.OrderID && a.Symbol == b.Symbol && a.Side == b.Side && a.PosSide == b.PosSide && a.At.Equal(b.At) && a.FeeCurrency == b.FeeCurrency && sameDecimal(a.Size, b.Size) && sameDecimal(a.Price, b.Price) && sameDecimal(a.Fee, b.Fee) && sameDecimal(a.PnL, b.PnL)
}

// Reconcile validates caller-supplied, order-attributed archive evidence. It
// neither fetches history nor establishes archive completeness itself. All sums
// remain exact decimals. Incomplete evidence never produces a reconciled net.
func Reconcile(in LedgerInput) LedgerReport {
	r := LedgerReport{Reconciliation: "pending", Costs: "unknown", Reasons: []string{}, Executions: []Execution{}}
	fail := func(code string) LedgerReport {
		r.Reconciliation = "mismatch"
		r.Costs = "unknown"
		r.Reasons = append(r.Reasons, code)
		sort.Strings(r.Reasons)
		r.Net = nil
		r.TradingNet = nil
		return r
	}
	expected, ok := decimal(in.EntrySize)
	if !ok || expected.Sign() <= 0 || !usdtSwap.MatchString(in.Symbol) || (in.Side != "long" && in.Side != "short") || in.EntryAt.IsZero() || in.Cutoff.Before(in.EntryAt) || (in.DBState != "open" && in.DBState != "closed") {
		return fail("invalid_ledger_scope")
	}
	if len(in.Fills) > 100000 || len(in.EntryOrderIDs) != 1 || len(in.ExitOrderIDs) > 10000 {
		return fail("ambiguous_or_unbounded_orders")
	}
	orders := map[string]string{}
	for role, ids := range map[string][]string{"entry": in.EntryOrderIDs, "exit": in.ExitOrderIDs} {
		for _, id := range ids {
			if id == "" || len(id) > 128 || orders[id] != "" {
				return fail("invalid_order_attribution")
			}
			orders[id] = role
		}
	}
	seen := map[string]Fill{}
	fills := make([]Fill, 0, len(in.Fills))
	for _, f := range in.Fills {
		if f.ID == "" || len(f.ID) > 128 {
			return fail("invalid_fill_identity")
		}
		if prior, exists := seen[f.ID]; exists {
			if !sameFill(prior, f) {
				return fail("conflicting_duplicate")
			}
			continue
		}
		seen[f.ID] = f
		fills = append(fills, f)
	}
	sort.Slice(fills, func(i, j int) bool {
		a, b := fills[i], fills[j]
		if !a.At.Equal(b.At) {
			return a.At.Before(b.At)
		}
		if orders[a.OrderID] != orders[b.OrderID] {
			return orders[a.OrderID] < orders[b.OrderID]
		}
		return a.ID < b.ID
	})
	entry, exit, gross, fees := new(big.Rat), new(big.Rat), new(big.Rat), new(big.Rat)
	feesKnown := true
	var firstEntry, lastEntry time.Time
	for _, f := range fills {
		role := orders[f.OrderID]
		wantSide := "buy"
		if (in.Side == "long" && role == "exit") || (in.Side == "short" && role == "entry") {
			wantSide = "sell"
		}
		if role == "" || f.Symbol != in.Symbol || f.Side != wantSide || (f.PosSide != in.Side && f.PosSide != "net") || f.At.IsZero() || f.At.Before(in.EntryAt) || f.At.After(in.Cutoff) {
			return fail("fill_attribution_mismatch")
		}
		size, sok := decimal(f.Size)
		price, pok := decimal(f.Price)
		fee, fok := decimal(f.Fee)
		pnl, rok := decimal(f.PnL)
		if !sok || !pok || !fok || !rok || size.Sign() <= 0 || price.Sign() <= 0 {
			return fail("invalid_execution_decimal")
		}
		if role == "entry" {
			if pnl.Sign() != 0 {
				return fail("entry_realized_pnl")
			}
			if firstEntry.IsZero() {
				firstEntry = f.At
			}
			lastEntry = f.At
			entry.Add(entry, size)
		} else {
			exit.Add(exit, size)
		}
		if entry.Cmp(expected) > 0 || exit.Cmp(entry) > 0 {
			return fail("volume_conservation_mismatch")
		}
		gross.Add(gross, pnl)
		var safeFee *string
		if f.FeeCurrency == "USDT" {
			fees.Add(fees, fee)
			v := decimalText(fee)
			safeFee = &v
		} else {
			feesKnown = false
		}
		r.Executions = append(r.Executions, Execution{len(r.Executions) + 1, role, f.At.UTC(), decimalText(size), decimalText(price), safeFee, decimalText(pnl)})
	}
	r.FillCount = len(fills)
	r.EntrySize = decimalText(entry)
	r.ExitSize = decimalText(exit)
	r.Gross = decimalText(gross)
	if feesKnown {
		r.Fees = decimalText(fees)
	} else {
		r.Reasons = append(r.Reasons, "fee_currency_unverified")
	}
	if !firstEntry.Equal(lastEntry) {
		r.Reasons = append(r.Reasons, "multi_time_entry")
	}
	if !in.ArchiveComplete {
		r.Reasons = append(r.Reasons, "archive_incomplete")
		sort.Strings(r.Reasons)
		return r
	}
	if entry.Cmp(expected) != 0 {
		return fail("entry_volume_mismatch")
	}
	remaining := new(big.Rat).Sub(entry, exit)
	r.Closed = remaining.Sign() == 0
	if (in.DBState == "closed") != r.Closed {
		return fail("db_state_mismatch")
	}
	if in.ExchangeRemaining != nil {
		v, ok := decimal(*in.ExchangeRemaining)
		if !ok || v.Sign() < 0 || v.Cmp(remaining) != 0 {
			return fail("exchange_volume_mismatch")
		}
	}
	r.Reconciliation = "matched"
	if feesKnown && r.Closed {
		net := new(big.Rat).Add(gross, fees)
		if in.RecordedTradingNet != nil {
			v, ok := decimal(*in.RecordedTradingNet)
			if !ok || v.Cmp(net) != 0 {
				return fail("recorded_net_mismatch")
			}
		}
		text := decimalText(net)
		r.TradingNet = &text
		r.Costs = "sensitivity-only"
		if f := in.Funding; f != nil && f.Complete && f.Currency == "USDT" {
			v, ok := decimal(f.Amount)
			if !ok {
				return fail("invalid_funding_decimal")
			}
			text := decimalText(new(big.Rat).Add(net, v))
			r.Net = &text
			r.Costs = "verified"
		} else {
			r.Reasons = append(r.Reasons, "funding_unverified")
		}
	}
	sort.Strings(r.Reasons)
	return r
}
