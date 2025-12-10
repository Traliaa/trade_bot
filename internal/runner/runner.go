package runner

import (
	"context"
	"sync"
	"time"
	"trade_bot/internal/models"

	okx_client "trade_bot/internal/modules/okx_client/service"
	okx_websocket "trade_bot/internal/modules/okx_websocket/service"
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

	mkt *okx_websocket.Client
	cfg *models.UserSettings
	mx  *okx_client.Client
	stg strategy.Engine
	n   TelegramNotifier

	queue       chan models.Signal
	pending     map[string]bool      // symbol -> awaiting decision
	cooldownTil map[string]time.Time // symbol -> until
	lastTick    map[string]time.Time // symbol -> last candle time

	mu       sync.Mutex // pending/cooldown
	healthMu sync.Mutex // lastTick
}

func New(user *models.UserSettings, n TelegramNotifier, mkt *okx_websocket.Client) *Runner {

	qsize := user.TradingSettings.ConfirmQueueMax
	if qsize <= 0 {
		qsize = 20
	}

	return &Runner{
		cfg:         user,
		mx:          okx_client.NewClient(user),
		n:           n,
		stg:         strategy.NewEngine(&user.TradingSettings),
		queue:       make(chan models.Signal, qsize),
		pending:     make(map[string]bool),
		cooldownTil: make(map[string]time.Time),
		lastTick:    make(map[string]time.Time),
		mkt:         mkt,
	}
}

//func (r *Runner) Start(ctx context.Context) {
//	// 1. Берём общий watchlist от стримера
//	watch := r.mkt.Watchlist()
//	if len(watch) == 0 {
//		r.n.SendF(ctx, r.cfg.UserID, "⚠️ Watchlist пуст, сигналы недоступны")
//		return
//	}
//
//	r.n.SendF(ctx, r.cfg.UserID, "📈 Watchlist для этого бота: %d символов", len(watch))
//
//	// 2. На каждый символ подписываемся на общий поток свечей
//	for _, sym := range watch {
//		sym := sym
//		ticks := r.mkt.Subscribe(sym)
//
//		go func() {
//			defer r.mkt.Unsubscribe(sym, ticks)
//
//			for {
//				select {
//				case <-ctx.Done():
//					return
//				case tick, ok := <-ticks:
//					if !ok {
//						return
//					}
//					// tick.InstID, tick.Close, tick.High, tick.Low ...
//					r.onCandle(ctx, tick)
//				}
//			}
//		}()
//	}
//
//	// плюс твой confirmWorker/healthLoop, как раньше
//	go r.confirmWorker(ctx)
//
//}

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

//func (r *Runner) onCandle(ctx context.Context, tick models.CandleTick) {
//	symbol := tick.InstID
//	now := time.Now()
//
//	r.healthMu.Lock()
//	r.lastTick[symbol] = now
//	r.healthMu.Unlock()
//
//	if r.cfg.TradingSettings.MaxOpenPositions > 0 {
//		if positions, err := r.mx.OpenPositions(ctx); err == nil &&
//			len(positions) >= r.cfg.TradingSettings.MaxOpenPositions {
//			return
//		}
//	}
//
//	log.Printf("[EVAL] %s candle-check close=%.6f", symbol, tick.Close)
//
//	sig := r.stg.OnCandle(symbol, strategy.Candle{
//		Open:  tick.Open,
//		High:  tick.High,
//		Low:   tick.Low,
//		Close: tick.Close,
//	})
//	if sig.Side == strategy.SideNone {
//		return
//	}
//
//	side := string(sig.Side)
//	price := sig.Price
//	if price <= 0 {
//		price = tick.Close
//	}
//
//	log.Printf("[STRAT] %s signal=%s @ %.6f | %s", symbol, side, price, sig.Reason)
//
//	r.mu.Lock()
//	defer r.mu.Unlock()
//
//	// 4. Кулдаун по символу
//	if until, ok := r.cooldownTil[symbol]; ok && now.Before(until) {
//		return
//	}
//
//	// 5. Уже ждёт подтверждения — не дублируем
//	if r.pending[symbol] {
//		return
//	}
//
//	req := signalReq{
//		symbol: symbol,
//		price:  price,
//		side:   side,
//	}
//
//	// 6. Пихаем сигнал в очередь с учётом политики
//	select {
//	case r.queue <- req:
//		log.Printf("[SIGNAL] %s %s @ %.4f", symbol, side, price)
//		r.pending[symbol] = true
//
//	default:
//		policy := r.cfg.TradingSettings.ConfirmQueuePolicy
//
//		switch policy {
//		case "drop_oldest":
//			select {
//			case <-r.queue:
//			default:
//			}
//			select {
//			case r.queue <- req:
//				log.Printf("[SIGNAL] %s %s @ %.4f (after drop_oldest)", symbol, side, price)
//				r.pending[symbol] = true
//			default:
//			}
//
//		case "drop_same_symbol":
//			return
//
//		default:
//			return
//		}
//	}
//}

//func (r *Runner) setPending(symbol string, v bool) {
//	r.mu.Lock()
//	r.pending[symbol] = v
//	r.mu.Unlock()
//}
//
//func (r *Runner) confirmWorker(ctx context.Context) {
//	for {
//		select {
//		case <-ctx.Done():
//			return
//
//		case req := <-r.queue:
//			// 0. Лимит открытых позиций
//			if r.cfg.TradingSettings.MaxOpenPositions > 0 {
//				if positions, err := r.mx.OpenPositions(ctx); err == nil &&
//					len(positions) >= r.cfg.TradingSettings.MaxOpenPositions {
//					r.setPending(req.symbol, false)
//					r.n.SendF(ctx, r.cfg.UserID,
//						"⚠️ [%s] Лимит открытых позиций (%d) достигнут, сигнал пропущен",
//						req.symbol, r.cfg.TradingSettings.MaxOpenPositions,
//					)
//					continue
//				}
//			}
//
//			prompt := fmt.Sprintf(
//				"🔔 [%s] SIGNAL %s @ %.4f\nSL/TP будут выставлены после входа. Войти?",
//				req.symbol, req.side, req.price,
//			)
//
//			ok := true
//			if r.cfg.TradingSettings.ConfirmRequired {
//				ok = r.n.Confirm(ctx, r.cfg.UserID, prompt, r.cfg.TradingSettings.ConfirmTimeout)
//			}
//			if !ok {
//				r.mu.Lock()
//				r.cooldownTil[req.symbol] = time.Now().Add(r.cfg.TradingSettings.CooldownPerSymbol)
//				r.mu.Unlock()
//				r.setPending(req.symbol, false)
//				r.n.SendF(ctx, r.cfg.UserID, "⛔️ [%s] Вход отменён/таймаут", req.symbol)
//				continue
//			}
//
//			// 1. Считаем все параметры сделки (SL/TP/size и т.д.)
//			params, err := r.calcTradeParams(ctx, req.symbol, req.side, req.price)
//			if err != nil {
//				r.n.SendF(ctx, r.cfg.UserID,
//					"❗️ [%s] Ошибка расчёта параметров сделки: %v", req.symbol, err)
//				r.setPending(req.symbol, false)
//				continue
//			}
//			r.n.SendF(ctx, r.cfg.UserID,
//				"[%s] DEBUG entry=%.6f SL=%.6f TP=%.6f 1R=%.6f RR=%.2f risk=%.2f%% size=%.4f",
//				req.symbol,
//				params.Entry, params.SL, params.TP, params.RiskDist,
//				params.RR, params.RiskPct, params.Size,
//			)
//
//			// 2. Открываем рыночный ордер
//			openType := 1
//			var sideInt int
//			if strings.EqualFold(params.Direction, "BUY") {
//				sideInt = 1
//			} else {
//				sideInt = 3
//			}
//
//			orderID, err := r.mx.PlaceMarket(
//				ctx, req.symbol, params.Size, sideInt,
//				params.Leverage, openType,
//			)
//			if err != nil {
//				r.n.SendF(ctx, r.cfg.UserID,
//					"❗️ [%s] Ошибка открытия ордера: %v", req.symbol, err)
//				r.setPending(req.symbol, false)
//				continue
//			}
//
//			// 3. TP/SL
//			posSide := "long"
//			if strings.EqualFold(params.Direction, "SELL") {
//				posSide = "short"
//			}
//
//			r.n.SendF(ctx, r.cfg.UserID,
//				"[%s] DEBUG entry=%.6f SL=%.6f TP=%.6f 1R=%.6f RR=%.2f risk=%.2f%% size=%.4f",
//				req.symbol,
//				params.Entry, params.SL, params.TP, params.RiskDist,
//				params.RR, params.RiskPct, params.Size,
//			)
//			// BUY => posSide="long", side="sell" (закрытие позиции)
//			side := "sell"
//
//			// 1) Stop-loss
//			err = r.mx.PlaceSingleAlgo(ctx, req.symbol, posSide, side, params.Size, params.SL, false)
//			if err != nil {
//				r.n.SendF(ctx, r.cfg.UserID,
//					"⚠️ [%s] TP/SL не выставлены на OKX: %v", req.symbol, err)
//			}
//
//			// 2) Take-profit
//			err = r.mx.PlaceSingleAlgo(ctx, req.symbol, posSide, side, params.Size, params.TP, true)
//			if err != nil {
//				r.n.SendF(ctx, r.cfg.UserID,
//					"⚠️ [%s] TP/SL не выставлены на OKX: %v", req.symbol, err)
//
//			}
//			//if err := r.mx.PlaceTpsl(ctx, req.symbol, posSide, params.Size, params.SL, params.TP); err != nil {
//			//	r.n.SendF(ctx, r.cfg.UserID,
//			//		"⚠️ [%s] TP/SL не выставлены на OKX: %v", req.symbol, err)
//			//}
//
//			r.n.SendF(ctx,
//				r.cfg.UserID,
//				"✅ [%s] Вход подтверждён | OPEN %-4s @ %.4f | SL=%.4f TP=%.4f lev=%dx size=%.4f | %s (orderId=%s)",
//				req.symbol, params.Direction, params.Entry, params.SL, params.TP,
//				params.Leverage, params.Size,
//				r.stg.Dump(req.symbol), orderID,
//			)
//
//			r.setPending(req.symbol, false)
//		}
//	}
//}

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
