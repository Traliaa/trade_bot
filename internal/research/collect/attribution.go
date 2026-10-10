//go:build linux || darwin

package collect

import (
	"strconv"
	"time"
)

// Explicit DB order links are necessary, not sufficient evidence. Only a closed
// position with fully conserved entry/exit volume can have a reconciled trading
// net. Public funding never certifies account charges; open positions stay pending.
func attributeEntry(e entryRecord, a FillArchive, w FillWindow) LedgerReport {
	pending := func(reason string) LedgerReport {
		return LedgerReport{Reconciliation: "pending", Costs: "unknown", Reasons: []string{reason}, Executions: []Execution{}}
	}
	if e.capture == nil || e.captureStatus != "complete" {
		return pending("capture_unverified")
	}
	in := e.capture
	if !e.linksComplete || e.orderID == "" {
		return pending("order_attribution_incomplete")
	}
	if in.Evidence.TimeSource != "exchange_fills" || in.Evidence.FirstFillAt == nil || in.Evidence.LastFillAt == nil || w.From.After(*in.Evidence.FirstFillAt) || !w.To.After(*in.Evidence.LastFillAt) {
		return pending("execution_window_unverified")
	}
	orders := map[string]bool{e.orderID: true}
	for _, id := range e.exitOrders {
		orders[id] = true
	}
	selected := []Fill{}
	for _, f := range a.fills {
		if orders[f.Fill.OrderID] {
			selected = append(selected, f.Fill)
		}
	}
	closedEvidence := e.state == "closed" && len(e.exitOrders) > 0 && a.Coverage.Status == "complete" && a.Coverage.RecordWindowComplete
	r := Reconcile(LedgerInput{Symbol: e.symbol, Side: in.Side, EntryAt: *in.Evidence.FirstFillAt, Cutoff: w.To.Add(-time.Millisecond), EntrySize: strconv.FormatFloat(in.Actual.Size, 'f', -1, 64), EntryOrderIDs: []string{e.orderID}, ExitOrderIDs: e.exitOrders, Fills: selected, ArchiveComplete: closedEvidence, DBState: e.state})
	// A bounded record archive cannot prove that unrelated/reused order evidence
	// belongs here; DB link conflicts are rejected before this projection.
	return r
}
