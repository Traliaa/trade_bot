package runner

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/models"

	"trade_bot/internal/exchange"
	"trade_bot/internal/strategy"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramNotifier interface {
	SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error)
	Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error)
	Confirm(ctx context.Context, chatID int64, prompt string, timeout time.Duration) bool
}
type signalReq struct {
	symbol string
	price  float64
	side   string
}

type Runner struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg *models.UserSettings
	mx  *exchange.Client
	stg *strategy.EMARSI
	n   TelegramNotifier

	queue       chan signalReq
	pending     map[string]bool      // symbol -> awaiting decision
	cooldownTil map[string]time.Time // symbol -> until
	lastTick    map[string]time.Time // symbol -> last candle time

	mu       sync.Mutex // pending/cooldown
	healthMu sync.Mutex // lastTick
}

func New(cfg *models.UserSettings, n TelegramNotifier) *Runner {
	mx := exchange.NewClient()

	mx.SetCreds(cfg.TradingSettings.OKXAPIKey, cfg.TradingSettings.OKXAPISecret, cfg.TradingSettings.OKXPassphrase)
	qsize := cfg.TradingSettings.ConfirmQueueMax
	if qsize <= 0 {
		qsize = 20
	}
	return &Runner{
		cfg:         cfg,
		mx:          mx,
		stg:         strategy.NewEMARSI(),
		n:           n,
		queue:       make(chan signalReq, qsize),
		pending:     make(map[string]bool),
		cooldownTil: make(map[string]time.Time),
		lastTick:    make(map[string]time.Time),
	}
}

func (r *Runner) Start(parent context.Context) {
	r.ctx, r.cancel = context.WithCancel(parent)
	// запуск воркера подтверждений
	go r.confirmWorker(r.ctx)
	// health-лог
	go r.healthLoop(r.ctx)

	raw := r.mx.TopVolatile(r.cfg.TradingSettings.WatchTopN)

	watch := []string{}
	for _, s := range raw {
		if r.mx.HasCandles(s, r.cfg.TradingSettings.Timeframe) {
			watch = append(watch, s)
		} else {
			log.Printf("[SKIP] %s — нет свечей %s у OKX", s, r.cfg.TradingSettings.Timeframe)
		}
	}
	if len(watch) == 0 {
		log.Println("[WATCHLIST] не удалось получить список самых волатильных инструментов")
		return
	}
	log.Printf("[WATCHLIST] топ %d самых волатильных SWAP: %v", len(watch), watch)
	r.n.SendF(r.ctx, r.cfg.UserID, "📈 Watchlist запущен: %d символов", len(watch))

	r.watchSymbols(r.ctx, watch)
}

func (r *Runner) watchSymbols(ctx context.Context, symbols []string) {
	log.Printf("[RUNNER] ▶️ Старт батч-отслеживания %d символов", len(symbols))
	stream := r.mx.StreamCandlesBatch(ctx, symbols, r.cfg.TradingSettings.Timeframe)
	for {
		select {
		case <-ctx.Done():
			return
		case tick, ok := <-stream:
			if !ok {
				return
			}
			log.Printf("[TICK] %s — %.4f", tick.InstID, tick.Close)
			r.onCandle(ctx, tick.InstID, tick.Close)
		}
	}
}

func (r *Runner) healthLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// считаем активные символы (те, по которым уже были свечи)
			r.healthMu.Lock()
			symbols := len(r.lastTick)
			r.healthMu.Unlock()

			// открытые позиции на OKX
			openCount := 0
			if positions, err := r.mx.OpenPositions(ctx); err == nil {
				openCount = len(positions)
			}

			qLen := len(r.queue)
			r.n.SendF(ctx, r.cfg.UserID, "🩺 HEALTH | symbols=%d | queue=%d | openPositions=%d", symbols, qLen, openCount)
		}
	}
}

// Stop — мягко гасит раннер.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

//func (r *Runner) runSymbol(ctx context.Context, symbol string) {
//	log.Printf("[RUNNER] ▶️ Старт отслеживания %s", symbol)
//	stream := r.mx.StreamCandles(ctx, symbol, r.cfg.Timeframe)
//	for {
//		select {
//		case <-ctx.Done():
//			return
//		case px, ok := <-stream:
//			if !ok {
//				return
//			}
//			log.Printf("[TICK] %s — %.4f", symbol, px)
//			r.onCandle(ctx, symbol, px)
//		}
//	}
//}

func (r *Runner) onCandle(ctx context.Context, symbol string, price float64) {
	// обновляем время последней свечи по символу (для health-лога)
	r.healthMu.Lock()
	r.lastTick[symbol] = time.Now()
	r.healthMu.Unlock()

	// лимит по открытым позициям на OKX
	if r.cfg.TradingSettings.MaxOpenPositions > 0 {
		if positions, err := r.mx.OpenPositions(ctx); err == nil && len(positions) >= r.cfg.TradingSettings.MaxOpenPositions {
			return
		}
	}

	log.Printf("[EVAL] %s candle-check", symbol)
	side, ok := r.stg.Update(
		symbol,
		price,
		r.cfg.TradingSettings.EMAShort,
		r.cfg.TradingSettings.EMALong,
		r.cfg.TradingSettings.RSIPeriod,
		r.cfg.TradingSettings.RSIOverbought,
		r.cfg.TradingSettings.RSIOSold,
	)
	if !ok {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// кулдаун по символу
	if until, ok := r.cooldownTil[symbol]; ok && time.Now().Before(until) {
		return
	}
	// если уже висит в ожидании — не добавляем
	if r.pending[symbol] {
		return
	}

	// попытка положить в очередь
	select {
	case r.queue <- signalReq{symbol: symbol, price: price, side: side}:
		log.Printf("[SIGNAL] %s %s @ %.4f", symbol, side, price)
		r.pending[symbol] = true
	default:
		policy := r.cfg.TradingSettings.ConfirmQueuePolicy
		if policy == "drop_oldest" {
			select {
			case <-r.queue:
			default:
			}
			select {
			case r.queue <- signalReq{symbol: symbol, price: price, side: side}:
				log.Printf("[SIGNAL] %s %s @ %.4f (after drop_oldest)", symbol, side, price)
				r.pending[symbol] = true
			default:
				// очередь переполнена
			}
		} else if policy == "drop_same_symbol" {
			// молча дропаем
			return
		} else {
			// по умолчанию просто не добавляем
			return
		}
	}
}

func (r *Runner) setPending(symbol string, v bool) {
	r.mu.Lock()
	r.pending[symbol] = v
	r.mu.Unlock()
}

func (r *Runner) confirmWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case req := <-r.queue:
			// 0. Лимит открытых позиций
			if r.cfg.TradingSettings.MaxOpenPositions > 0 {
				if positions, err := r.mx.OpenPositions(ctx); err == nil &&
					len(positions) >= r.cfg.TradingSettings.MaxOpenPositions {
					r.setPending(req.symbol, false)
					r.n.SendF(ctx, r.cfg.UserID,
						"⚠️ [%s] Лимит открытых позиций (%d) достигнут, сигнал пропущен",
						req.symbol, r.cfg.TradingSettings.MaxOpenPositions,
					)
					continue
				}
			}

			prompt := fmt.Sprintf(
				"🔔 [%s] SIGNAL %s @ %.4f\nSL/TP будут выставлены после входа. Войти?",
				req.symbol, req.side, req.price,
			)

			ok := true
			if r.cfg.TradingSettings.ConfirmRequired {
				ok = r.n.Confirm(ctx, r.cfg.UserID, prompt, r.cfg.TradingSettings.ConfirmTimeout)
			}
			if !ok {
				r.mu.Lock()
				r.cooldownTil[req.symbol] = time.Now().Add(r.cfg.TradingSettings.CooldownPerSymbol)
				r.mu.Unlock()
				r.setPending(req.symbol, false)
				r.n.SendF(ctx, r.cfg.UserID, "⛔️ [%s] Вход отменён/таймаут", req.symbol)
				continue
			}

			// 1. Считаем все параметры сделки (SL/TP/size и т.д.)
			params, err := r.calcTradeParams(ctx, req.symbol, req.side, req.price)
			if err != nil {
				r.n.SendF(ctx, r.cfg.UserID,
					"❗️ [%s] Ошибка расчёта параметров сделки: %v", req.symbol, err)
				r.setPending(req.symbol, false)
				continue
			}

			// 2. Открываем рыночный ордер
			openType := 1
			var sideInt int
			if strings.EqualFold(params.Direction, "BUY") {
				sideInt = 1
			} else {
				sideInt = 3
			}

			orderID, err := r.mx.PlaceMarket(
				ctx, req.symbol, params.Size, sideInt,
				params.Leverage, openType,
			)
			if err != nil {
				r.n.SendF(ctx, r.cfg.UserID,
					"❗️ [%s] Ошибка открытия ордера: %v", req.symbol, err)
				r.setPending(req.symbol, false)
				continue
			}

			// 3. TP/SL
			posSide := "long"
			if strings.EqualFold(params.Direction, "SELL") {
				posSide = "short"
			}

			r.n.SendF(ctx, r.cfg.UserID,
				"[%s] DEBUG entry=%.6f SL=%.6f TP=%.6f 1R=%.6f RR=%.2f risk=%.2f%% size=%.4f",
				req.symbol,
				params.Entry, params.SL, params.TP, params.RiskDist,
				params.RR, params.RiskPct, params.Size,
			)

			if err := r.mx.PlaceTpsl(ctx, req.symbol, posSide, params.SL, params.TP); err != nil {
				r.n.SendF(ctx, r.cfg.UserID,
					"⚠️ [%s] TP/SL не выставлены на OKX: %v", req.symbol, err)
			}

			r.n.SendF(ctx,
				r.cfg.UserID,
				"✅ [%s] Вход подтверждён | OPEN %-4s @ %.4f | SL=%.4f TP=%.4f lev=%dx size=%.4f | %s (orderId=%s)",
				req.symbol, params.Direction, params.Entry, params.SL, params.TP,
				params.Leverage, params.Size,
				r.stg.Dump(req.symbol), orderID,
			)

			r.setPending(req.symbol, false)
		}
	}
}

// TradeParams содержит все рассчитанные параметры сделки.
type TradeParams struct {
	Entry     float64
	SL        float64
	TP        float64
	Size      float64
	TickSize  float64
	RiskPct   float64
	RR        float64
	RiskDist  float64
	Leverage  int
	Direction string // "BUY" или "SELL"
}

// calcTradeParams считает SL, TP, размер позиции и сопутствующие параметры
// по текущим настройкам стратегии.
func (r *Runner) calcTradeParams(
	ctx context.Context,
	symbol string,
	side string,
	entry float64,
) (*TradeParams, error) {
	side = strings.ToUpper(side)

	// 1. Настройки риска
	riskPct := r.cfg.TradingSettings.RiskPct / 100.0 // 3 => 0.03
	if riskPct <= 0 {
		return nil, fmt.Errorf("riskPct <= 0")
	}
	rr := r.cfg.TradingSettings.TakeProfitRR
	if rr <= 0 {
		rr = 3.0
	}
	lev := r.cfg.TradingSettings.Leverage

	// 2. Забираем мету инструмента (включая tickSize)
	price, stepSize, minSz, tickSize, err := r.mx.GetInstrumentMeta(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("GetInstrumentMeta: %w", err)
	}
	if entry <= 0 {
		entry = price
	}
	if entry <= 0 {
		return nil, fmt.Errorf("entry <= 0")
	}

	// 3. Считаем сырой SL
	var sl float64
	if side == "BUY" {
		sl = entry * (1 - riskPct)
	} else {
		sl = entry * (1 + riskPct)
	}

	// 4. Округляем SL по tickSize
	sl = roundToTick(sl, tickSize)

	// 5. 1R и TP (1R считаем уже по округлённому SL)
	riskDist := math.Abs(entry - sl)

	var tp float64
	if side == "BUY" {
		tp = entry + rr*riskDist
	} else {
		tp = entry - rr*riskDist
	}
	// Округляем TP
	tp = roundToTick(tp, tickSize)

	// 6. Считаем размер позиции с учётом того SL, который реально уйдёт на биржу
	size, err := r.calcSizeByRiskWithMeta(ctx, symbol, entry, sl, stepSize, minSz, tickSize)
	if err != nil {
		return nil, fmt.Errorf("calcSizeByRisk: %w", err)
	}
	if size <= 0 {
		return nil, fmt.Errorf("size <= 0")
	}

	params := &TradeParams{
		Entry:     entry,
		SL:        sl,
		TP:        tp,
		Size:      size,
		TickSize:  tickSize,
		RiskPct:   r.cfg.TradingSettings.RiskPct,
		RR:        rr,
		RiskDist:  riskDist,
		Leverage:  lev,
		Direction: side,
	}
	return params, nil
}
func roundToTick(px, tick float64) float64 {
	if tick <= 0 {
		return px
	}
	steps := math.Round(px/tick + 1e-9)
	return steps * tick
}
func (r *Runner) calcSizeByRiskWithMeta(
	ctx context.Context,
	symbol string,
	entryPrice float64,
	slPrice float64,
	stepSize float64,
	minSz float64,
	tickSize float64,
) (float64, error) {

	if entryPrice <= 0 || slPrice <= 0 {
		return 0, fmt.Errorf("entry/sl <= 0")
	}

	// дистанция до стопа
	stopDist := math.Abs(entryPrice - slPrice)
	if stopDist <= 0 {
		return 0, fmt.Errorf("нулевой стоп")
	}
	stopPct := stopDist / entryPrice

	// equity
	equity, err := r.mx.USDTBalance(ctx)
	if err != nil {
		return 0, fmt.Errorf("get equity: %w", err)
	}
	if equity <= 0 {
		return 0, fmt.Errorf("equity <= 0")
	}

	riskFraction := r.cfg.TradingSettings.RiskPct / 100.0
	if riskFraction <= 0 {
		return 0, fmt.Errorf("riskFraction <= 0")
	}
	riskUSDT := equity * riskFraction

	// сколько должна стоить позиция
	positionValue := riskUSDT / stopPct

	// ограничение плечом
	lev := float64(r.cfg.TradingSettings.Leverage)
	if lev > 0 {
		maxPositionValue := equity * lev
		if positionValue > maxPositionValue {
			positionValue = maxPositionValue
		}
	}

	rawSz := positionValue / entryPrice

	// приводим к minSz
	if rawSz < minSz {
		rawSz = minSz
	}

	steps := math.Floor(rawSz/stepSize + 1e-9)
	sz := steps * stepSize
	if sz <= 0 {
		return 0, fmt.Errorf("ноль после округления")
	}

	return sz, nil
}
