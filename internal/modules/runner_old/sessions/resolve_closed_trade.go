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

	execution, err := s.resolveClosedTradeExecution(ctx, tr)
	if err != nil {
		return models.TradeCloseInput{}, err
	}
	exitPrice, exitSize, exitAt := execution.ExitPrice, execution.ExitSize, execution.ExitAt

	// Защита от битого учёта закрытия.
	if p.EntrySize > 0 && exitSize > p.EntrySize {
		return models.TradeCloseInput{}, fmt.Errorf(
			"invalid close size: guid=%s inst=%s exit_size=%.8f > entry_size=%.8f",
			tr.GUID,
			tr.InstID,
			exitSize,
			p.EntrySize,
		)
	}

	state, _ := s.getTrailStateForTrade(tr)
	reason := classifyCloseReason(tr, p, state, execution.FinalFillPrice)

	payload := p
	payload.ExitPrice = exitPrice
	payload.ExitSize = exitSize
	payload.DurationSec = models.CalcDurationSec(tr.EntryAt, &exitAt)

	if payload.RiskDist <= 0 {
		payload.RiskDist = models.CalcRiskDist(payload.EntryPrice, payload.StopLoss, payload.PosSide)
	}

	if payload.StopLoss > 0 && exitPrice > 0 {
		payload.ExitPriceR = models.CalcRMultiple(
			payload.EntryPrice,
			exitPrice,
			payload.StopLoss,
			payload.PosSide,
		)
		payload.RMultiple = payload.ExitPriceR
	}

	realizedPnL, priceMovePct, realizedPnLPct := calcClosedTradeMetrics(payload)
	payload.GrossRealizedPnL = execution.GrossRealizedPnL
	if len(execution.Fills) == 0 {
		payload.GrossRealizedPnL = realizedPnL
	}
	payload.TotalFees += execution.TotalFees
	payload.RealizedPnL = payload.GrossRealizedPnL + payload.TotalFees
	if payload.PlannedRiskUSDT > 0 {
		payload.EffectiveR = payload.RealizedPnL / payload.PlannedRiskUSDT
		payload.RMultiple = payload.EffectiveR
	}
	payload.PriceMovePct = priceMovePct
	payload.RealizedPnLPct = realizedPnLPct
	if pct := calcNetRealizedPnLPct(payload); pct != 0 {
		payload.RealizedPnLPct = pct
	}

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

	if len(execution.Fills) > 0 {
		if err := s.Repo.UpsertTradeFills(ctx, tradeFillRecordsForSession(tr, execution.Fills, models.TradeFillRoleExit)); err != nil {
			return models.TradeCloseInput{}, fmt.Errorf("persist exit fills: %w", err)
		}
	}

	return models.TradeCloseInput{
		ExitAt:      exitAt,
		CloseReason: reason,
		Payload:     payload,
	}, nil
}

type closedTradeExecution struct {
	ExitPrice        float64
	FinalFillPrice   float64
	ExitSize         float64
	ExitAt           time.Time
	GrossRealizedPnL float64
	TotalFees        float64
	Fills            []models.TradeFill
}

func (s *UserSession) resolveClosedTradeExecution(
	ctx context.Context,
	tr models.TradeRecord,
) (closedTradeExecution, error) {
	p := tr.Payload

	fills, ferr := s.Okx.RecentFills(ctx, tr.InstID, 100)
	if ferr == nil {
		closeFills := pickCloseFills(fills, tr)
		if len(closeFills) > 0 {
			var execution closedTradeExecution
			var notional float64
			for _, fill := range closeFills {
				notional += fill.FillPx * fill.FillSz
				execution.ExitSize += fill.FillSz
				execution.GrossRealizedPnL += fill.RealizedPnL
				execution.TotalFees += fill.Fee
			}
			last := closeFills[len(closeFills)-1]
			execution.FinalFillPrice = last.FillPx
			execution.ExitAt = last.FillTime
			execution.Fills = closeFills
			if execution.ExitSize > 0 {
				execution.ExitPrice = notional / execution.ExitSize
			}
			return execution, nil
		}
	}

	// Если фактический fill не нашли, допускаем только trade-local fallback,
	// уже сохранённый в payload. Не используем CurrentSize/CurrentPrice:
	// они могут быть агрегированным snapshot по общей позиции символа.
	if p.ExitPrice > 0 && p.ExitSize > 0 {
		return closedTradeExecution{
			ExitPrice:      p.ExitPrice,
			FinalFillPrice: p.ExitPrice,
			ExitSize:       p.ExitSize,
			ExitAt:         time.Now().UTC(),
		}, nil
	}

	return closedTradeExecution{}, fmt.Errorf(
		"close execution not resolved: guid=%s inst=%s entry_size=%.8f current_size=%.8f",
		tr.GUID,
		tr.InstID,
		p.EntrySize,
		p.CurrentSize,
	)
}

func tradeFillRecordsForSession(trade models.TradeRecord, fills []models.TradeFill, role models.TradeFillRole) []models.TradeFillRecord {
	out := make([]models.TradeFillRecord, 0, len(fills))
	for _, fill := range fills {
		out = append(out, models.TradeFillRecord{
			TradeGUID:   trade.GUID,
			TradeID:     fill.TradeID,
			OrderID:     fill.OrderID,
			AlgoID:      fill.AlgoID,
			InstID:      fill.InstID,
			PosSide:     fill.PosSide,
			Side:        fill.Side,
			Role:        role,
			FillPrice:   fill.FillPx,
			FillSize:    fill.FillSz,
			Fee:         fill.Fee,
			RealizedPnL: fill.RealizedPnL,
			FilledAt:    fill.FillTime,
		})
	}
	return out
}

func calcNetRealizedPnLPct(p models.TradePayload) float64 {
	if p.EntryPrice <= 0 || p.EntrySize <= 0 || p.RealizedPnL == 0 {
		return 0
	}
	sizeBase := p.EntrySize
	if p.CtVal > 0 {
		sizeBase *= p.CtVal
	}
	entryNotional := p.EntryPrice * sizeBase
	if entryNotional <= 0 {
		return 0
	}
	if p.Leverage > 0 {
		entryNotional /= float64(p.Leverage)
	}
	if entryNotional <= 0 {
		return 0
	}
	return p.RealizedPnL / entryNotional * 100
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
	_ = tr

	if payload.PendingCloseReason != "" {
		return models.NormalizeCloseReason(payload.PendingCloseReason)
	}

	const epsMul = 0.4 // БЫЛО: 0.15. Увеличили для лучшего распознавания при проскальзывании

	riskDist := payload.RiskDist
	if riskDist <= 0 {
		riskDist = models.CalcRiskDist(payload.EntryPrice, payload.StopLoss, payload.PosSide)
	}

	if riskDist > 0 {
		eps := riskDist * epsMul

		// 1. Сначала фактическое закрытие около уровней TP / SL.
		if payload.TakeProfit > 0 && approxLevel(exitPrice, payload.TakeProfit, eps) {
			return models.CloseReasonTP
		}
		if payload.StopLoss > 0 && approxLevel(exitPrice, payload.StopLoss, eps) {
			return models.CloseReasonSL
		}

		// 2. Потом break-even по фактическому уровню.
		if payload.MovedToBE {
			if payload.BEPrice > 0 && approxLevel(exitPrice, payload.BEPrice, eps) {
				return models.CloseReasonBreakEven
			}
			// fallback на entry для старых сделок
			if payload.BEPrice <= 0 && approxLevel(exitPrice, payload.EntryPrice, eps) {
				return models.CloseReasonBreakEven
			}
		}

		// 3. Потом lock-profit, только если реально закрылись в плюс.
		// ... (логика lock profit)
	}

	// 4. Time stop — если был взведен флаг в payload.
	// БОТ ТЕПЕРЬ ВСЕГДА ПРИЗНАЕТ TIMESTOP, ЕСЛИ ОН БЫЛ ВЗВЕДЕН В PAYLOAD
	if payload.TimeStopTriggered {
		return models.CloseReasonTimeStopStale
	}

	// 5. Состояния trail/state как fallback, чтобы не терять классификацию.
	if payload.IsStale || (state != nil && state.IsStale) {
		return models.CloseReasonTimeStopStale
	}
	if payload.TookPartial || (state != nil && state.TookPartial) {
		return models.CloseReasonPartialExit
	}
	if payload.LockedProfit || (state != nil && state.LockedProfit) {
		return models.CloseReasonLockProfit
	}
	if payload.MovedToBE || (state != nil && state.MovedToBE) {
		return models.CloseReasonBreakEven
	}

	// 6. Последний fallback: если есть валидный exit относительно entry/stop,
	// пытаемся классифицировать по знаку и масштабу результата вместо unknown.
	if riskDist > 0 && payload.EntryPrice > 0 && exitPrice > 0 {
		r := models.CalcRMultiple(
			payload.EntryPrice,
			exitPrice,
			payload.StopLoss,
			payload.PosSide,
		)
		if r >= 0.8 {
			return models.CloseReasonTP
		}
		if r <= -0.8 {
			return models.CloseReasonSL
		}
		if r > 0 {
			return models.CloseReasonLockProfit
		}
		if r > -0.25 {
			return models.CloseReasonBreakEven
		}
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
