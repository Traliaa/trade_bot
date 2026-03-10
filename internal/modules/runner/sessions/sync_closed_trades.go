package sessions

import (
	"context"
	"fmt"
	"math"
	"strconv"
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

func (s *UserSession) resolveClosedTrade(ctx context.Context, tr models.TradeRecord) (models.TradeCloseInput, error) {
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

		// Берём самый поздний fill после открытия.
		if best == nil {
			best = &fills[i]
			continue
		}
		bestTs, _ := strconv.ParseInt(best.FillTime, 10, 64)
		if fillTimeMs > bestTs {
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
	const eps = 0.001 // 0.1%

	if almostEqual(exitPx, tr.TakeProfit, eps) {
		return models.CloseReasonTP
	}
	if almostEqual(exitPx, tr.StopLoss, eps) {
		return models.CloseReasonSL
	}

	// TimeStop ты уже знаешь в своей логике — при явном time stop
	// можно передавать reason в локальный state и сохранять отдельно.
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
func formatClosedTradeMessage(tr models.TradeRecord, in models.TradeCloseInput) string {
	var b strings.Builder

	title := "✅ СДЕЛКА ЗАКРЫТА"
	switch in.CloseReason {
	case models.CloseReasonTP:
		title = "🎯 ТЕЙК-ПРОФИТ"
	case models.CloseReasonSL:
		title = "🛑 СТОП-ЛОСС"
	case models.CloseReasonTimeStop:
		title = "🕒 TIME STOP"
	}

	b.WriteString(title + "\n\n")
	fmt.Fprintf(&b, "Инструмент: %s\n", tr.InstID)
	fmt.Fprintf(&b, "Направление: %s\n", strings.ToUpper(tr.PosSide))
	fmt.Fprintf(&b, "Стратегия: %s\n", tr.Strategy)
	fmt.Fprintf(&b, "ТФ: %s\n\n", tr.Timeframe)

	fmt.Fprintf(&b, "Вход: %.6f\n", tr.EntryPrice)
	fmt.Fprintf(&b, "Выход: %.6f\n", in.ExitPrice)
	fmt.Fprintf(&b, "Размер: %.4f\n\n", in.ExitSize)

	fmt.Fprintf(&b, "PnL: %+.2f USDT\n", in.RealizedPnL)
	fmt.Fprintf(&b, "Результат: %+.2f%%\n", in.RealizedPnLPct)
	fmt.Fprintf(&b, "Причина: %s\n", in.CloseReason)

	if tr.ExitAt != nil {
		dur := in.ExitAt.Sub(tr.EntryAt).Round(time.Minute)
		fmt.Fprintf(&b, "Длительность: %s\n", dur)
	}

	return b.String()
}
