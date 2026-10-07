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
	exitSize := execution.ExitSize

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

	closeInput := closedTradeInput(tr, execution)
	if len(execution.Fills) > 0 {
		if err := s.Repo.UpsertTradeFills(ctx, tradeFillRecordsForSession(tr, execution.Fills, models.TradeFillRoleExit)); err != nil {
			return models.TradeCloseInput{}, fmt.Errorf("persist exit fills: %w", err)
		}
	}
	return closeInput, nil
}

// Pure report finalization, shared by production and regression fixtures.
func closedTradeInput(tr models.TradeRecord, execution closedTradeExecution) models.TradeCloseInput {
	p := tr.Payload
	exitPrice, exitSize, exitAt := execution.ExitPrice, execution.ExitSize, execution.ExitAt
	reason, source := classifyCloseEvidence(p, nil, execution.FinalFillPrice)

	payload := p
	payload.CloseIntentReason = p.PendingCloseReason
	payload.CloseReasonSource = source
	payload.ExitPrice = exitPrice
	payload.ExitSize = exitSize
	payload.DurationSec = models.CalcDurationSec(tr.EntryAt, &exitAt)

	payload.RiskDist = payload.InitialRiskDist()
	payload.ExitPriceR = models.CalcPriceR(payload.EntryPrice, exitPrice, payload.RiskDist, payload.PosSide)
	payload.RMultiple = payload.ExitPriceR

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
		payload.MFER = models.CalcPriceR(
			payload.EntryPrice,
			payload.MFEPrice,
			payload.RiskDist,
			payload.PosSide,
		)
	}
	if payload.MAEPrice > 0 {
		payload.MAER = models.CalcPriceR(
			payload.EntryPrice,
			payload.MAEPrice,
			payload.RiskDist,
			payload.PosSide,
		)
	}

	// Закрытие уже финализировано, pending больше не нужен.
	payload.PendingCloseReason = ""

	return models.TradeCloseInput{
		ExitAt:      exitAt,
		CloseReason: reason,
		Payload:     payload,
	}
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
			if err := validateCloseFillIdentities(fills, closeFills); err != nil {
				return closedTradeExecution{}, err
			}
			// Reject ambiguous/invalid fills before persisting anything. Never mask
			// an invalid exchange response with the payload fallback below.
			return aggregateCloseExecution(closeFills, p.EntrySize)
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
	reason, _ := classifyCloseEvidence(payload, state, exitPrice)
	return reason
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
