package sessions

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"trade_bot/internal/models"
)

func (s *UserSession) resolveClosedTrade(
	ctx context.Context,
	tr models.TradeRecord,
) (models.TradeCloseInput, error) {
	fills, err := s.Okx.TransactionDetails(ctx, "SWAP", tr.InstID, 100)
	if err != nil {
		return models.TradeCloseInput{}, err
	}

	var best *models.TransactionDetailRecord

	for i := range fills {
		f := fills[i]

		if !strings.EqualFold(f.InstID, tr.InstID) {
			continue
		}
		if tr.PosSide != "" && !strings.EqualFold(f.PosSide, tr.PosSide) {
			continue
		}

		fillTimeMs, _ := strconv.ParseInt(f.FillTime, 10, 64)
		fillTime := time.UnixMilli(fillTimeMs)

		if fillTime.Before(tr.EntryAt.Add(-1 * time.Minute)) {
			continue
		}

		if best == nil {
			best = &fills[i]
			continue
		}

		bestTimeMs, _ := strconv.ParseInt(best.FillTime, 10, 64)
		if fillTimeMs > bestTimeMs {
			best = &fills[i]
		}
	}

	if best == nil {
		return models.TradeCloseInput{}, fmt.Errorf("close fill not found")
	}

	exitPx, _ := strconv.ParseFloat(best.FillPx, 64)
	exitSz, _ := strconv.ParseFloat(best.FillSz, 64)
	fillPnl, _ := strconv.ParseFloat(best.FillPnl, 64)
	fillTimeMs, _ := strconv.ParseInt(best.FillTime, 10, 64)
	exitAt := time.UnixMilli(fillTimeMs)

	pnlPct := 0.0
	if tr.EntryPrice > 0 && tr.EntrySize > 0 {
		notional := tr.EntryPrice * tr.EntrySize
		if notional > 0 {
			pnlPct = fillPnl / notional * 100
		}
	}

	reason := detectCloseReason(tr, exitPx)

	if state, ok := s.getPositionState(tr.InstID, tr.PosSide); ok {
		if state.CloseReason != "" {
			reason = models.CloseReason(state.CloseReason)
		}
	}

	return models.TradeCloseInput{
		ExitPrice:      exitPx,
		ExitSize:       exitSz,
		ExitAt:         exitAt,
		RealizedPnL:    fillPnl,
		RealizedPnLPct: pnlPct,
		CloseReason:    reason,
	}, nil
}

func detectCloseReason(tr models.TradeRecord, exitPx float64) models.CloseReason {
	const eps = 0.001

	if almostEqual(exitPx, tr.TakeProfit, eps) {
		return models.CloseReasonTP
	}
	if almostEqual(exitPx, tr.StopLoss, eps) {
		return models.CloseReasonSL
	}

	return models.CloseReasonUnknown
}

func almostEqual(a, b, rel float64) bool {
	if a == 0 || b == 0 {
		return false
	}

	diff := math.Abs(a - b)
	base := math.Max(math.Abs(a), math.Abs(b))

	return diff/base <= rel
}
