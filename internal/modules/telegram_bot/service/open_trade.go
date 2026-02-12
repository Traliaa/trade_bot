package service

import (
	"context"
	"math"
	"strings"
	"trade_bot/internal/models"
)

func (t *Telegram) openTestTradeBTC1x(ctx context.Context, chatID int64) {
	session, err := t.router.GetSession(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	// торговые креды именно пользователя
	ts := session.User.Settings.TradingSettings
	if strings.TrimSpace(ts.OKXAPIKey) == "" ||
		strings.TrimSpace(ts.OKXAPISecret) == "" ||
		strings.TrimSpace(ts.OKXPassphrase) == "" {
		_, _ = t.Send(ctx, chatID, "🔑 Для тестовой сделки нужны OKX ключ/секрет/пасфраза. Добавь их и повтори.")
		return
	}

	instID := "BTC-USDT-SWAP"
	direction := "BUY" // можно позже сделать выбор BUY/SELL двумя кнопками
	leverage := 1

	// 1) Получаем цену и мету инструмента (LastPx уже внутри)
	inst, err := session.Okx.GetInstrumentMeta(ctx, instID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "⚠️ Не удалось получить данные инструмента BTC: "+err.Error())
		return
	}
	entry := inst.LastPx
	if entry <= 0 {
		_, _ = t.Send(ctx, chatID, "⚠️ Некорректная цена BTC (LastPx).")
		return
	}

	// 2) SL/TP из StopPct и TakeProfitRR (если пусто — дефолты)
	stopPct := ts.StopPct
	if stopPct <= 0 {
		stopPct = 1.2
	}
	rr := ts.TakeProfitRR
	if rr <= 0 {
		rr = 2.0
	}

	riskDist := entry * stopPct / 100.0 // 1R в цене

	var sl, tp float64
	if strings.EqualFold(direction, "BUY") {
		sl = entry - riskDist
		tp = entry + riskDist*rr
	} else {
		sl = entry + riskDist
		tp = entry - riskDist*rr
	}

	// 3) Размер: берём минимально допустимый по инструменту (minSz), округляя к lotSz
	size := normalizeSize(inst.MinSz, inst.LotSz, inst.MinSz)

	params := &models.TradeParams{
		Direction: direction,
		Size:      size,
		Leverage:  leverage,

		Entry:    entry,
		SL:       sl,
		TP:       tp,
		RiskDist: riskDist,
		RR:       rr,
		RiskPct:  ts.RiskPct, // чисто для DEBUG-строки
	}

	sig := models.Signal{
		InstID:   instID,
		Strategy: "TEST",
		Reason:   "manual_test_btc_1x",
	}

	_, err = session.OpenPositionWithTpSl(ctx, sig, params)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "❗️Тестовая сделка не открылась: "+err.Error())
		return
	}
}

func normalizeSize(v, lotSz, minSz float64) float64 {
	if lotSz <= 0 {
		lotSz = 1
	}
	if minSz <= 0 {
		minSz = lotSz
	}
	if v < minSz {
		v = minSz
	}
	// округляем вверх до шага lotSz
	n := math.Ceil(v/lotSz) * lotSz
	// защита от 0 из-за NaN/Inf
	if n <= 0 {
		return minSz
	}
	return n
}
