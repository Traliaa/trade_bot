package sessions

import (
	"context"
	"math"
	"strings"
	"time"
	"trade_bot/internal/models"

	"go.uber.org/zap"
)

func (s *UserSession) SyncClosedTrades(ctx context.Context) error {
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
		posSide := strings.ToLower(p.Side)
		if posSide == "" {
			posSide = "long"
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
			currentSize := openPositionSize(pos, tr)

			payload.CurrentPrice = currentPrice
			payload.CurrentSize = currentSize
			payload.DurationSec = int64(now.Sub(tr.EntryAt).Seconds())

			payload.UnrealizedPnL = openPositionUnrealizedPnL(pos, payload, currentPrice)
			payload.UnrealizedPnLPct = openPositionUnrealizedPnLPct(pos, payload, currentPrice)
			payload.PriceMovePct = calcPriceMovePct(payload, currentPrice)
			payload.CtVal = pos.CtVal
			if pos.UnrealizedPct != 0 {
				payload.ExchangeUPLRatio = pos.UnrealizedPct
			}

			// partial tracking
			if payload.EntrySize > 0 && currentSize > 0 && currentSize < payload.EntrySize {
				payload.TookPartial = true
				payload.ClosedSize = payload.EntrySize - currentSize
				if payload.PartialCount == 0 {
					payload.PartialCount = 1
				}
			}

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
func approxLevel(exitPrice, level, eps float64) bool {
	if exitPrice <= 0 || level <= 0 || eps <= 0 {
		return false
	}
	return math.Abs(exitPrice-level) <= eps
}
func calcPriceMovePct(p models.TradePayload, currentPrice float64) float64 {
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
func (s *UserSession) syncTradeCloseIntent(
	ctx context.Context,
	st *models.PositionTrailState,
	reason models.CloseReason,
) error {
	if st == nil {
		return nil
	}

	return s.syncTradePayloadFromTrail(ctx, st.InstID, st.PosSide, func(p *models.TradePayload) {
		p.MovedToBE = st.MovedToBE
		p.LockedProfit = st.LockedProfit
		p.TookPartial = st.TookPartial
		p.PendingCloseReason = string(reason)

		if st.MovedToBE && st.SL > 0 {
			p.BEPrice = st.SL
		}

		if reason == models.CloseReasonTimeStop || reason == models.CloseReasonTimeStopEarly || reason == models.CloseReasonTimeStopStale {
			p.TimeStopTriggered = true
		}
	})
}

func (s *UserSession) syncTradeFlagsFromState(
	ctx context.Context,
	st *models.PositionTrailState,
	currentSize float64,
) error {
	if st == nil {
		return nil
	}

	return s.syncTradePayloadFromTrail(ctx, st.InstID, st.PosSide, func(p *models.TradePayload) {
		p.MovedToBE = st.MovedToBE
		p.LockedProfit = st.LockedProfit
		p.TookPartial = st.TookPartial

		p.IsStale = st.IsStale
		p.StaleSince = st.StaleSince
		p.StaleMarkedAtR = st.StaleMarkedAtR

		if st.MovedToBE && st.SL > 0 {
			p.BEPrice = st.SL
		}

		if currentSize > 0 {
			p.CurrentSize = currentSize
			if p.EntrySize > 0 && currentSize < p.EntrySize {
				p.TookPartial = true
				p.ClosedSize = p.EntrySize - currentSize
				if p.PartialCount == 0 {
					p.PartialCount = 1
				}
			}
		}
	})
}
