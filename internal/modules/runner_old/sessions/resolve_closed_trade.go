package sessions

import (
	"context"
	"fmt"
	"math"
	"time"

	"trade_bot/internal/models"
)

func (s *UserSession) resolveClosedTrade(
	ctx context.Context,
	tr models.TradeRecord,
) (models.TradeCloseInput, error) {
	_ = ctx

	p := tr.Payload

	exitPrice, exitSize, exitAt, err := s.resolveClosedTradeExecution(ctx, tr)
	if err != nil {
		return models.TradeCloseInput{}, err
	}

	state, _ := s.getTrailStateForTrade(tr)
	reason := classifyCloseReason(tr, p, state, exitPrice)

	payload := p
	payload.ExitPrice = exitPrice
	payload.ExitSize = exitSize
	payload.DurationSec = models.CalcDurationSec(tr.EntryAt, &exitAt)

	if payload.RiskDist <= 0 {
		payload.RiskDist = models.CalcRiskDist(payload.EntryPrice, payload.StopLoss, payload.PosSide)
	}

	if payload.StopLoss > 0 && exitPrice > 0 {
		payload.RMultiple = models.CalcRMultiple(
			payload.EntryPrice,
			exitPrice,
			payload.StopLoss,
			payload.PosSide,
		)
	}

	realizedPnL, realizedPnLPct := calcClosedTradePnL(payload)
	payload.RealizedPnL = realizedPnL
	payload.RealizedPnLPct = realizedPnLPct

	if payload.MFEPrice > 0 {
		payload.MFER = models.CalcMFER(
			payload.EntryPrice,
			payload.MFEPrice,
			payload.StopLoss,
			payload.PosSide,
		)
	}
	if payload.MAEPrice > 0 {
		payload.MAER = models.CalcMAER(
			payload.EntryPrice,
			payload.MAEPrice,
			payload.StopLoss,
			payload.PosSide,
		)
	}

	return models.TradeCloseInput{
		ExitAt:      exitAt,
		CloseReason: reason,
		Payload:     payload,
	}, nil
}

func (s *UserSession) resolveClosedTradeExecution(
	ctx context.Context,
	tr models.TradeRecord,
) (exitPrice float64, exitSize float64, exitAt time.Time, err error) {
	p := tr.Payload

	fills, ferr := s.Okx.RecentFills(ctx, tr.InstID, 20)
	if ferr == nil {
		if fill := pickCloseFill(fills, tr); fill != nil {
			return fill.FillPx, fill.FillSz, fill.FillTime, nil
		}
	}

	now := time.Now().UTC()

	if p.CurrentPrice > 0 {
		exitPrice = p.CurrentPrice
	} else if p.MAEPrice > 0 {
		exitPrice = p.MAEPrice
	} else if p.MFEPrice > 0 {
		exitPrice = p.MFEPrice
	} else {
		exitPrice = p.EntryPrice
	}

	if p.CurrentSize > 0 {
		exitSize = p.CurrentSize
	} else if p.EntrySize > 0 {
		exitSize = p.EntrySize
	} else {
		return 0, 0, time.Time{}, fmt.Errorf("invalid trade payload: entry_size=%.8f current_size=%.8f", p.EntrySize, p.CurrentSize)
	}

	exitAt = now
	return exitPrice, exitSize, exitAt, nil
}

func (s *UserSession) getTrailStateForTrade(tr models.TradeRecord) (*models.PositionTrailState, bool) {
	key := models.PosKey{
		InstID:  tr.InstID,
		PosSide: tr.Payload.PosSide,
	}

	s.TrailMu.RLock()
	defer s.TrailMu.RUnlock()

	st, ok := s.TrailStates[key]
	return st, ok
}

func classifyCloseReason(
	tr models.TradeRecord,
	payload models.TradePayload,
	state *models.PositionTrailState,
	exitPrice float64,
) models.CloseReason {

	if payload.PendingCloseReason != "" {
		return models.NormalizeCloseReason(payload.PendingCloseReason)
	}

	const epsMul = 0.15

	riskDist := payload.RiskDist
	if riskDist <= 0 {
		riskDist = models.CalcRiskDist(payload.EntryPrice, payload.StopLoss, payload.PosSide)
	}
	if riskDist <= 0 {
		return models.CloseReasonUnknown
	}

	eps := riskDist * epsMul

	// 1. TP / SL по близости к уровням
	if payload.TakeProfit > 0 && math.Abs(exitPrice-payload.TakeProfit) <= eps {
		return models.CloseReasonTP
	}
	if payload.StopLoss > 0 && math.Abs(exitPrice-payload.StopLoss) <= eps {
		return models.CloseReasonSL
	}

	// 2. Runtime flags
	if state != nil {
		if state.LockedProfit {
			switch payload.PosSide {
			case "long":
				if exitPrice > payload.EntryPrice {
					return models.CloseReasonLockProfit
				}
			case "short":
				if exitPrice < payload.EntryPrice {
					return models.CloseReasonLockProfit
				}
			}
		}

		if state.MovedToBE && math.Abs(exitPrice-payload.EntryPrice) <= eps {
			return models.CloseReasonBreakEven
		}

		if state.TookPartial {
			return models.CloseReasonPartialExit
		}
	}

	// 3. Payload flags как fallback
	if payload.LockedProfit {
		switch payload.PosSide {
		case "long":
			if exitPrice > payload.EntryPrice {
				return models.CloseReasonLockProfit
			}
		case "short":
			if exitPrice < payload.EntryPrice {
				return models.CloseReasonLockProfit
			}
		}
	}

	if payload.MovedToBE && math.Abs(exitPrice-payload.EntryPrice) <= eps {
		return models.CloseReasonBreakEven
	}

	if payload.TookPartial {
		return models.CloseReasonPartialExit
	}

	return models.CloseReasonUnknown
}

func calcClosedTradePnL(p models.TradePayload) (float64, float64) {
	if p.EntryPrice <= 0 || p.ExitPrice <= 0 || p.ExitSize <= 0 {
		return 0, 0
	}

	var pnl float64
	switch p.PosSide {
	case "long":
		pnl = (p.ExitPrice - p.EntryPrice) * p.ExitSize
	case "short":
		pnl = (p.EntryPrice - p.ExitPrice) * p.ExitSize
	default:
		return 0, 0
	}

	// Человеческий percent move, а не uplRatio OKX
	pct := 0.0
	if p.EntryPrice > 0 {
		switch p.PosSide {
		case "long":
			pct = ((p.ExitPrice - p.EntryPrice) / p.EntryPrice) * 100
		case "short":
			pct = ((p.EntryPrice - p.ExitPrice) / p.EntryPrice) * 100
		}
	}

	return pnl, pct
}
