package sessions

import (
	"context"
	"strings"
	"time"
	"trade_bot/internal/models"

	"go.uber.org/zap"
)

func (s *UserSession) syncClosedTrades(ctx context.Context) error {
	openTrades, err := s.Repo.ListOpenTrades(ctx, s.User.TelegramID)
	if err != nil {
		return err
	}
	if len(openTrades) == 0 {
		return nil
	}

	openPos, err := s.Okx.OpenPositions(ctx)
	if err != nil {
		return err
	}

	okxOpen := make(map[string]models.OpenPosition, len(openPos))
	for _, p := range openPos {
		posSide := "long"
		if strings.EqualFold(p.Side, "short") {
			posSide = "short"
		}
		okxOpen[tradeKey(p.Symbol, posSide)] = p
	}

	now := time.Now().UTC()

	for _, tr := range openTrades {
		key := tradeKey(tr.InstID, tr.Payload.PosSide)
		pos, stillOpen := okxOpen[key]
		if stillOpen {
			payload := tr.Payload

			currentPrice := openPositionCurrentPrice(pos, tr)
			payload.CurrentPrice = currentPrice
			payload.DurationSec = int64(now.Sub(tr.EntryAt).Seconds())
			payload.ExitSize = openPositionSize(pos, tr)

			payload.UnrealizedPnL = openPositionUnrealizedPnL(pos, payload, currentPrice)
			payload.UnrealizedPnLPct = openPositionUnrealizedPnLPct(pos, payload, currentPrice)

			if payload.StopLoss > 0 && currentPrice > 0 {
				payload.RMultiple = models.CalcRMultiple(
					payload.EntryPrice,
					currentPrice,
					payload.StopLoss,
					payload.PosSide,
				)
			}

			switch payload.PosSide {
			case "long":
				if currentPrice > payload.MFEPrice {
					payload.MFEPrice = currentPrice
				}
				if payload.MAEPrice == 0 || currentPrice < payload.MAEPrice {
					payload.MAEPrice = currentPrice
				}
			case "short":
				if payload.MFEPrice == 0 || currentPrice < payload.MFEPrice {
					payload.MFEPrice = currentPrice
				}
				if currentPrice > payload.MAEPrice {
					payload.MAEPrice = currentPrice
				}
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

			if err := s.Repo.UpdatePayload(ctx, tr.GUID, payload); err != nil {
				s.Logger.Warn("update open trade payload failed",
					zap.Error(err),
					zap.String("tradeID", tr.GUID.String()),
					zap.String("instId", tr.InstID),
					zap.String("posSide", tr.Payload.PosSide),
				)
			}

			continue
		}

		closeInput, err := s.resolveClosedTrade(ctx, tr)
		if err != nil {
			s.Logger.Warn("resolve closed trade failed",
				zap.Error(err),
				zap.String("instId", tr.InstID),
				zap.String("posSide", tr.Payload.PosSide),
			)
			continue
		}

		if err := s.Repo.CloseTrade(ctx, tr.GUID, closeInput); err != nil {
			s.Logger.Error("close trade in history failed",
				zap.Error(err),
				zap.String("tradeID", tr.GUID.String()),
			)
			continue
		}

		s.Notifier.Send(ctx, s.User.TelegramID, formatClosedTradeMessage(tr, closeInput))
	}

	return nil
}

func tradeKey(instID, posSide string) string {
	return instID + ":" + posSide
}
func calcUnrealizedPnL(p models.TradePayload, currentPrice float64) float64 {
	if p.EntryPrice <= 0 || currentPrice <= 0 || p.EntrySize <= 0 {
		return 0
	}

	switch p.PosSide {
	case "long":
		return (currentPrice - p.EntryPrice) * p.EntrySize
	case "short":
		return (p.EntryPrice - currentPrice) * p.EntrySize
	default:
		return 0
	}
}

func calcUnrealizedPnLPct(p models.TradePayload, currentPrice float64) float64 {
	if p.EntryPrice <= 0 || currentPrice <= 0 {
		return 0
	}

	switch p.PosSide {
	case "long":
		return ((currentPrice - p.EntryPrice) / p.EntryPrice) * 100
	case "short":
		return ((p.EntryPrice - currentPrice) / p.EntryPrice) * 100
	default:
		return 0
	}
}
func openPositionCurrentPrice(pos models.OpenPosition, tr models.TradeRecord) float64 {
	if pos.MarkPx > 0 {
		return pos.MarkPx
	}
	if pos.Last > 0 {
		return pos.Last
	}
	if pos.AvgPx > 0 {
		return pos.AvgPx
	}
	return tr.Payload.EntryPrice
}

func openPositionSize(pos models.OpenPosition, tr models.TradeRecord) float64 {
	if pos.Size > 0 {
		return pos.Size
	}
	return tr.Payload.EntrySize
}

func openPositionUnrealizedPnL(pos models.OpenPosition, payload models.TradePayload, currentPrice float64) float64 {
	if pos.UnrealizedPnL != 0 {
		return pos.UnrealizedPnL
	}
	return calcUnrealizedPnL(payload, currentPrice)
}

func openPositionUnrealizedPnLPct(pos models.OpenPosition, payload models.TradePayload, currentPrice float64) float64 {
	if pos.UnrealizedPct != 0 {
		return pos.UnrealizedPct * 100
	}
	return calcUnrealizedPnLPct(payload, currentPrice)
}
