package pg

import (
	"math"
	"time"
	"trade_bot/internal/models"
)

type tradeReportSnapshot struct {
	Trade models.TradeRecord
	Fills []models.TradeFillRecord
}

func buildReconciledTradeStats(snapshots []tradeReportSnapshot, checkedAt time.Time) models.TradeStats {
	closed := make([]models.TradeRecord, 0, len(snapshots))
	report := &models.TradeReconciliationReport{Scope: "stored_fills", FundingStatus: "not_checked", CheckedAt: checkedAt, Trades: []models.TradeReconciliation{}}
	report.VerifiedStats = &models.ReconciledMoneyStats{}
	report.VerifiedStatsStatus = "no_verified_trades"
	var openCount int64
	var openPnL, profit, loss float64
	for _, snapshot := range snapshots {
		tr := snapshot.Trade
		if tr.Status == models.TradeStatusOpen {
			openCount++
			openPnL += tr.Payload.UnrealizedPnL
			continue
		}
		if tr.Status != models.TradeStatusClosed {
			continue
		}
		closed = append(closed, tr)
		r := models.ReconcileTrade(tr, snapshot.Fills)
		report.Trades = append(report.Trades, r)
		switch r.Status {
		case "incomplete":
			report.Incomplete++
		case "mismatch":
			report.Mismatched++
		case "verified":
			report.Verified++
			v := report.VerifiedStats
			v.Trades++
			v.NetPnL += r.NetPnL
			v.Fees += r.Fees
			switch {
			case r.NetPnL > 0:
				v.Wins++
				profit += r.NetPnL
			case r.NetPnL < 0:
				v.Losses++
				loss -= r.NetPnL
			default:
				v.Breakeven++
			}
		}
	}
	v := report.VerifiedStats
	if v.Trades > 0 {
		report.VerifiedStatsStatus = "available"
		v.WinRate = float64(v.Wins) / float64(v.Trades) * 100
		v.AvgPnL = v.NetPnL / float64(v.Trades)
	}
	if loss > 0 {
		pf := profit / loss
		v.ProfitFactor = &pf
	}
	values := []float64{v.NetPnL, v.Fees, v.AvgPnL, profit, loss}
	if v.ProfitFactor != nil {
		values = append(values, *v.ProfitFactor)
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			report.VerifiedStats = nil
			report.VerifiedStatsStatus = "numeric_overflow"
			break
		}
	}
	stats := buildTradeStats(closed)
	stats.OpenTrades = openCount
	stats.TotalTrades += openCount
	stats.OpenPnL = openPnL
	stats.Reconciliation = report
	return stats
}
