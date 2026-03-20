package sessions

import (
	"context"
	"fmt"
	"time"
	"trade_bot/internal/helper"

	"trade_bot/internal/models"
)

func (s *UserSession) resolveClosedTrade(
	ctx context.Context,
	tr models.TradeRecord,
) (models.TradeCloseInput, error) {
	// Берём самую свежую версию трейда, чтобы не словить гонку:
	// PendingCloseReason / TimeStopTriggered / BEPrice могли обновиться уже после ListOpenTrades.
	if fresh, err := s.Repo.GetByGUID(ctx, tr.GUID); err == nil && fresh != nil {
		tr = *fresh
	}

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

	realizedPnL, priceMovePct, realizedPnLPct := calcClosedTradeMetrics(payload)
	payload.RealizedPnL = realizedPnL
	payload.PriceMovePct = priceMovePct
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

	// Закрытие уже финализировано, pending больше не нужен.
	payload.PendingCloseReason = ""

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
	key := helper.TrailKey(tr.InstID, tr.Payload.PosSide)

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

	if riskDist > 0 {
		eps := riskDist * epsMul

		// 1. TP / SL по близости к уровням
		if payload.TakeProfit > 0 && approxLevel(exitPrice, payload.TakeProfit, eps) {
			return models.CloseReasonTP
		}
		if payload.StopLoss > 0 && approxLevel(exitPrice, payload.StopLoss, eps) {
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

			// если есть сохранённый фактический BEPrice в payload — используем его
			if payload.MovedToBE {
				if payload.BEPrice > 0 && approxLevel(exitPrice, payload.BEPrice, eps) {
					return models.CloseReasonBreakEven
				}
				// fallback на entry для старых сделок
				if payload.BEPrice <= 0 && approxLevel(exitPrice, payload.EntryPrice, eps) {
					return models.CloseReasonBreakEven
				}
			}

			if state.TookPartial {
				return models.CloseReasonPartialExit
			}
		}

		// 3. Payload flags
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

		if payload.MovedToBE {
			if payload.BEPrice > 0 && approxLevel(exitPrice, payload.BEPrice, eps) {
				return models.CloseReasonBreakEven
			}
			if payload.BEPrice <= 0 && approxLevel(exitPrice, payload.EntryPrice, eps) {
				return models.CloseReasonBreakEven
			}
		}

		if payload.TookPartial {
			return models.CloseReasonPartialExit
		}
	} else {
		// Даже без riskDist не валимся сразу в unknown
		if payload.LockedProfit {
			return models.CloseReasonLockProfit
		}
		if payload.MovedToBE {
			return models.CloseReasonBreakEven
		}
		if payload.TookPartial {
			return models.CloseReasonPartialExit
		}
	}

	if payload.TimeStopTriggered {
		return models.CloseReasonTimeStopStale
	}

	return models.CloseReasonUnknown
}
func calcClosedTradeMetrics(p models.TradePayload) (realizedPnL, priceMovePct, realizedPnLPct float64) {
	if p.EntryPrice <= 0 || p.ExitPrice <= 0 || p.ExitSize <= 0 {
		return 0, 0, 0
	}

	// Для OKX swap ExitSize приходит в контрактах.
	// Если есть CtVal, переводим в base qty.
	sizeBase := p.ExitSize
	if p.CtVal > 0 {
		sizeBase = p.ExitSize * p.CtVal
	}

	switch p.PosSide {
	case "long":
		realizedPnL = (p.ExitPrice - p.EntryPrice) * sizeBase
		priceMovePct = ((p.ExitPrice - p.EntryPrice) / p.EntryPrice) * 100

	case "short":
		realizedPnL = (p.EntryPrice - p.ExitPrice) * sizeBase
		priceMovePct = ((p.EntryPrice - p.ExitPrice) / p.EntryPrice) * 100

	default:
		return 0, 0, 0
	}

	// Выбираем базу для pnl%.
	// Если есть плечо — считаем от margin, это ближе к OKX ROI.
	// Иначе fallback на notional.
	entryNotional := p.EntryPrice * sizeBase
	if entryNotional <= 0 {
		return realizedPnL, priceMovePct, 0
	}

	if p.Leverage > 0 {
		margin := entryNotional / float64(p.Leverage)
		if margin > 0 {
			realizedPnLPct = (realizedPnL / margin) * 100
			return realizedPnL, priceMovePct, realizedPnLPct
		}
	}

	realizedPnLPct = (realizedPnL / entryNotional) * 100
	return realizedPnL, priceMovePct, realizedPnLPct
}
