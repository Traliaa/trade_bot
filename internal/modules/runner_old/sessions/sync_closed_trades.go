package sessions

import (
	"context"
	"strings"
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
	s.USDTBalance(ctx)

	okxOpen := make(map[string]models.OpenPosition, len(openPos))
	for _, p := range openPos {
		posSide := "long"
		if strings.EqualFold(p.Side, "short") {
			posSide = "short"
		}
		okxOpen[tradeKey(p.Symbol, posSide)] = p
	}

	for _, tr := range openTrades {
		key := tradeKey(tr.InstID, tr.PosSide)
		if _, stillOpen := okxOpen[key]; stillOpen {
			continue
		}

		closeInput, err := s.resolveClosedTrade(ctx, tr)
		if err != nil {
			s.Logger.Warn("resolve closed trade failed",
				zap.Error(err),
				zap.String("instId", tr.InstID),
				zap.String("posSide", tr.PosSide),
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
