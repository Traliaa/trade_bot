package sessions

import (
	"context"
	"fmt"
	"time"

	"trade_bot/internal/models"
)

func (s *UserSession) resolveClosedTrade(
	ctx context.Context,
	tr models.TradeRecord,
) (models.TradeCloseInput, error) {
	_ = ctx

	p := tr.Payload

	// здесь дальше ты можешь заменить на реальный источник:
	// - last filled order
	// - closed position info from OKX
	// - algo order status
	// - fallback market price
	exitPrice, exitSize, exitAt, reason, err := s.resolveClosedTradeExecution(ctx, tr)
	if err != nil {
		return models.TradeCloseInput{}, err
	}

	payload := p
	payload.ExitPrice = exitPrice
	payload.ExitSize = exitSize
	payload.DurationSec = models.CalcDurationSec(tr.EntryAt, &exitAt)
	payload.RiskDist = models.CalcRiskDist(payload.EntryPrice, payload.StopLoss, payload.PosSide)
	payload.RMultiple = models.CalcRMultiple(payload.EntryPrice, payload.ExitPrice, payload.StopLoss, payload.PosSide)

	realizedPnL, realizedPnLPct := calcClosedTradePnL(payload)
	payload.RealizedPnL = realizedPnL
	payload.RealizedPnLPct = realizedPnLPct

	if payload.MFEPrice > 0 {
		payload.MFER = models.CalcMFER(payload.EntryPrice, payload.MFEPrice, payload.StopLoss, payload.PosSide)
	}
	if payload.MAEPrice > 0 {
		payload.MAER = models.CalcMAER(payload.EntryPrice, payload.MAEPrice, payload.StopLoss, payload.PosSide)
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
) (exitPrice float64, exitSize float64, exitAt time.Time, reason models.CloseReason, err error) {
	_ = ctx

	p := tr.Payload

	// Временный fallback.
	// Потом сюда можно подставить:
	// - получение последних fills с OKX
	// - определение sl/tp/time stop/manual/recovery
	// Сейчас хотя бы не ломаем пайплайн.

	if p.EntryPrice <= 0 || p.EntrySize <= 0 {
		return 0, 0, time.Time{}, models.CloseReasonUnknown, fmt.Errorf("invalid trade payload: entry_price=%.8f entry_size=%.8f", p.EntryPrice, p.EntrySize)
	}

	return p.EntryPrice, p.EntrySize, time.Now().UTC(), models.CloseReasonUnknown, nil
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

	// Это price move %, без плеча.
	// Если хочешь отдельный leveraged pnl pct — лучше считать и хранить отдельно.
	pnlPct := 0.0
	if p.EntryPrice > 0 {
		switch p.PosSide {
		case "long":
			pnlPct = ((p.ExitPrice - p.EntryPrice) / p.EntryPrice) * 100
		case "short":
			pnlPct = ((p.EntryPrice - p.ExitPrice) / p.EntryPrice) * 100
		}
	}

	return pnl, pnlPct
}
