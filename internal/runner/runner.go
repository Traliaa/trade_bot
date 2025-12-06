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
			// ещё раз проверяем лимит позиций перед входом
			if r.cfg.TradingSettings.MaxOpenPositions > 0 {
				if positions, err := r.mx.OpenPositions(ctx); err == nil && len(positions) >= r.cfg.TradingSettings.MaxOpenPositions {
					r.setPending(req.symbol, false)
					r.n.SendF(ctx, r.cfg.UserID, "⚠️ [%s] Лимит открытых позиций (%d) достигнут, сигнал пропущен", req.symbol, r.cfg.TradingSettings.MaxOpenPositions)
					continue
				}
			}

			prompt := fmt.Sprintf("🔔 [%s] SIGNAL %s @ %.4f\nSL/TP будут выставлены после входа. Войти?", req.symbol, req.side, req.price)

			ok := true
			if r.cfg.TradingSettings.ConfirmRequired {
				// подтверждение живёт своей жизнью, не завязано на общий ctx
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

			instID := req.symbol // у тебя в логах уже вида MON-USDT-SWAP

			// 1. Получаем equity и мету инструмента
			price, stepSize, minSz, err := r.mx.GetInstrumentMeta(ctx, instID)
			if err != nil {
				r.n.SendF(ctx, r.cfg.UserID, "❗️ [%s] Ошибка получения параметров инструмента: %v", req.symbol, err)
				r.setPending(req.symbol, false)
				continue
			}

			// 2. Считаем размер позиции из процента баланса
			riskPercent := r.cfg.TradingSettings.RiskPct // например 1.0 => 1% баланса
			riskFraction := riskPercent / 100.0          // Превращаем в долю [0..1]
			leverage := float64(r.cfg.TradingSettings.Leverage)
			sz, err := r.calcSizeByRisk(ctx, riskFraction, price, leverage, stepSize, minSz)
			if sz <= 0 {
				r.n.SendF(ctx, r.cfg.UserID, "❗️ [%s] Некорректный размер позиции (sz=%.8f)", req.symbol, sz)
				r.setPending(req.symbol, false)
				continue
			}

			// 3. Risk-management по цене: отступ SL/TP
			// r.cfg.RiskPct — процент от цены (например 1%)
			priceRisk := req.price * (r.cfg.TradingSettings.RiskPct / 100.0)

			var sl, tp float64
			if strings.EqualFold(req.side, "BUY") {
				sl = req.price - priceRisk
				tp = req.price + 3*priceRisk
			} else {
				sl = req.price + priceRisk
				tp = req.price - 3*priceRisk
			}

			// 4. Открываем рыночный ордер на рассчитанный объём
			openType := 1 // как у тебя и было
			var sideInt int
			if strings.EqualFold(req.side, "BUY") {
				sideInt = 1
			} else {
				sideInt = 3
			}

			orderID, err := r.mx.PlaceMarket(ctx, instID, sz, sideInt, r.cfg.TradingSettings.Leverage, openType)
			if err != nil {
				r.n.SendF(ctx, r.cfg.UserID, "❗️ [%s] Ошибка открытия ордера: %v", req.symbol, err)
				r.setPending(req.symbol, false)
				continue
			}

			// 5. Вешаем TP/SL через order-algo
			posSide := "long"
			if strings.EqualFold(req.side, "SELL") {
				posSide = "short"
			}
			r.n.SendF(ctx, r.cfg.UserID, "[%s] DEBUG SL=%.6f TP=%.6f side=%s", req.symbol, sl, tp, req.side)

			if err := r.mx.PlaceTpsl(ctx, instID, posSide, sl, tp); err != nil {
				r.n.SendF(ctx, r.cfg.UserID, "⚠️ [%s] TP/SL не выставлены на OKX: %v", req.symbol, err)
				// позиция уже открыта, поэтому pending всё равно снимаем
			}

			r.n.SendF(ctx,
				r.cfg.UserID, "✅ [%s] Вход подтверждён | OPEN %-4s @ %.4f | SL=%.4f TP=%.4f lev=%dx size=%.4f | %s (orderId=%s)",
				req.symbol, req.side, req.price, sl, tp, r.cfg.TradingSettings.Leverage, sz, r.stg.Dump(req.symbol), orderID,
			)

			r.setPending(req.symbol, false)
		}
	}
}

// stepSize и minSz можно взять из /public/instruments
func (r *Runner) calcSizeByRisk(ctx context.Context, riskFraction, price, leverage, stepSize, minSz float64) (float64, error) {

	equity, err := r.mx.USDTBalance(ctx)
	if err != nil {
		return 0, err
	}

	if equity <= 0 {
		return 0, fmt.Errorf("")
	}
	if price <= 0 {
		return 0, fmt.Errorf("")
	}
	if leverage <= 0 {
		return 0, fmt.Errorf("")
	}

	// 1. Сколько USDT мы готовы потерять по SL
	riskUSDT := equity * riskFraction // 400 * 0.01 = 4 USDT

	// 2. Размер позиции в деньгах с учётом плеча
	positionValue := riskUSDT * leverage // 4 * 20 = 80 USDT позиции

	// 3. Сырой размер в контрактах
	rawSz := positionValue / price

	// 4. Приводим к шагу и минимуму
	if rawSz < minSz {
		rawSz = minSz
	}

	steps := math.Floor(rawSz/stepSize + 1e-9)
	sz := steps * stepSize
	if sz <= 0 {
		return 0, nil
	}
	return sz, nil
}

// Stop — мягко гасит раннер.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}
