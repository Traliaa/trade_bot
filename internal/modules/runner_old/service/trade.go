package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"trade_bot/internal/models"
)

type OpenTradeMessage struct {
	Symbol     string
	Timeframe  string
	Side       string
	Entry      float64
	StopLoss   float64
	TakeProfit float64
	RR         float64
	RiskPct    float64
	Size       float64
	Leverage   int
	Strategy   string
	Signal     string
	Time       time.Time
}

type CloseTradeMessage struct {
	Symbol    string
	Side      string
	Entry     float64
	Exit      float64
	ResultPct float64
	PnLUSDT   float64
	Duration  time.Duration
	Reason    string
}

type PartialTradeMessage struct {
	Symbol        string
	Side          string
	Entry         float64
	CurrentPrice  float64
	ClosedPct     float64
	PartialPnL    float64
	RemainingSize float64
	NewStop       float64
	Reason        string
}

func formatOpenPositionMessage(
	sig models.Signal,
	res *models.OpenResult,
	params *models.TradeParams,
) string {
	var b strings.Builder

	riskPct := 0.0
	if res.Entry > 0 {
		riskPct = math.Abs(res.Entry-params.SL) / res.Entry * 100
	}

	rr := 0.0
	riskDist := math.Abs(res.Entry - params.SL)
	rewardDist := math.Abs(params.TP - res.Entry)
	if riskDist > 0 {
		rr = rewardDist / riskDist
	}

	b.WriteString("🚀 ОТКРЫТА ПОЗИЦИЯ\n\n")
	fmt.Fprintf(&b, "Инструмент: %s\n", sig.InstID)
	fmt.Fprintf(&b, "Направление: %s\n", strings.ToUpper(string(sig.Side)))
	fmt.Fprintf(&b, "ТФ: %s\n", sig.TF)
	if sig.Strategy != "" {
		fmt.Fprintf(&b, "Стратегия: %s\n", sig.Strategy)
	}

	b.WriteString("\n")
	fmt.Fprintf(&b, "Вход: %s\n", formatPrice(res.Entry))
	fmt.Fprintf(&b, "Размер: %s\n", formatQty(params.Size))

	b.WriteString("\n")
	fmt.Fprintf(&b, "SL: %s\n", formatPrice(params.SL))
	fmt.Fprintf(&b, "TP: %s\n", formatPrice(params.TP))
	fmt.Fprintf(&b, "Риск: %.2f%%\n", riskPct)
	fmt.Fprintf(&b, "RR: %.2f\n", rr)

	b.WriteString("\n")
	fmt.Fprintf(&b, "Время: %s", time.Now().Format("15:04:05"))

	return b.String()
}

func formatCloseTrade(m CloseTradeMessage) string {
	var b strings.Builder

	title := "✅ СДЕЛКА ЗАКРЫТА"
	if m.Reason == "stop_loss" {
		title = "🛑 СТОП-ЛОСС"
	} else if m.Reason == "take_profit" {
		title = "🎯 ТЕЙК-ПРОФИТ"
	}

	b.WriteString(title + "\n\n")
	fmt.Fprintf(&b, "Инструмент: %s\n", m.Symbol)
	fmt.Fprintf(&b, "Направление: %s\n\n", strings.ToUpper(m.Side))

	fmt.Fprintf(&b, "Вход: %s\n", formatPrice(m.Entry))
	fmt.Fprintf(&b, "Выход: %s\n\n", formatPrice(m.Exit))

	fmt.Fprintf(&b, "Результат: %+.2f%%\n", m.ResultPct)
	fmt.Fprintf(&b, "PnL: %+.2f USDT\n", m.PnLUSDT)

	if m.Duration > 0 {
		fmt.Fprintf(&b, "Длительность: %s\n", formatDuration(m.Duration))
	}
	if m.Reason != "" {
		fmt.Fprintf(&b, "Причина: %s\n", m.Reason)
	}

	return b.String()
}

func formatPartialTrade(m PartialTradeMessage) string {
	var b strings.Builder

	b.WriteString("💸 ЧАСТИЧНАЯ ФИКСАЦИЯ\n\n")
	fmt.Fprintf(&b, "Инструмент: %s\n", m.Symbol)
	fmt.Fprintf(&b, "Направление: %s\n\n", strings.ToUpper(m.Side))

	fmt.Fprintf(&b, "Вход: %s\n", formatPrice(m.Entry))
	fmt.Fprintf(&b, "Текущая цена: %s\n\n", formatPrice(m.CurrentPrice))

	fmt.Fprintf(&b, "Закрыто: %.0f%%\n", m.ClosedPct)
	fmt.Fprintf(&b, "PnL по части: %+.2f USDT\n", m.PartialPnL)

	if m.RemainingSize > 0 {
		fmt.Fprintf(&b, "Остаток позиции: %s\n", formatQty(m.RemainingSize))
	}
	if m.NewStop > 0 {
		fmt.Fprintf(&b, "Стоп перенесён: %s\n", formatPrice(m.NewStop))
	}
	if m.Reason != "" {
		fmt.Fprintf(&b, "Причина: %s\n", m.Reason)
	}

	return b.String()
}

func formatPrice(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func formatQty(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return d.Truncate(time.Second).String()
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}
