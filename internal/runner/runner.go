package runner

import (
	"context"
	"fmt"
	"sync"
	"time"
	"trade_bot/internal/config"
	"trade_bot/internal/exchange"
	"trade_bot/internal/notify"
	"trade_bot/internal/strategy"
)

type signalReq struct {
	symbol string
	price  float64
	side   string
}

type Runner struct {
	cfg *config.Config
	mx  *exchange.MexcClient
	stg *strategy.EMARSI
	n   notify.Notifier

	queue       chan signalReq
	pending     map[string]bool      // symbol -> awaiting decision
	cooldownTil map[string]time.Time // symbol -> until
	mu          sync.Mutex
}

func New(cfg *config.Config, mx *exchange.MexcClient, stg *strategy.EMARSI, n notify.Notifier) *Runner {
	mx.SetCreds(cfg.MexcAPIKey, cfg.MexcAPISecret)
	qsize := cfg.ConfirmQueueMax
	if qsize <= 0 {
		qsize = 20
	}
	return &Runner{
		cfg:         cfg,
		mx:          mx,
		stg:         stg,
		n:           n,
		queue:       make(chan signalReq, qsize),
		pending:     make(map[string]bool),
		cooldownTil: make(map[string]time.Time),
	}
}

func (r *Runner) Start(ctx context.Context) {
	// воркер подтверждений
	go r.confirmWorker(ctx)

	watch := r.mx.TopVolatile(r.cfg.DefaultWatchTopN)
	r.n.Sendf("📈 Watchlist запущен: %d символов", len(watch))

	var wg sync.WaitGroup
	for _, sym := range watch {
		s := sym
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.runSymbol(ctx, s)
		}()
	}

	go func() {
		wg.Wait()
		r.n.Send("🛑 Все стримы закрыты")
	}()
}

func (r *Runner) runSymbol(ctx context.Context, symbol string) {
	stream := r.mx.StreamPrices(ctx, symbol)
	for {
		select {
		case <-ctx.Done():
			return
		case px, ok := <-stream:
			if !ok {
				return
			}
			r.onTick(ctx, symbol, px)
		}
	}
}

func (r *Runner) onTick(ctx context.Context, symbol string, price float64) {
	side, ok := r.stg.Update(symbol, price,
		r.cfg.EMAShort, r.cfg.EMALong,
		r.cfg.RSIPeriod, r.cfg.RSIOverbought, r.cfg.RSIOSold)
	if !ok {
		return
	}

	// локальное состояние позиций больше не храним — всё берём с биржи
	// здесь просто ставим сигнал в очередь, если нет кулдауна/пенднига

	r.mu.Lock()
	// кулдаун по символу
	if until, ok := r.cooldownTil[symbol]; ok && time.Now().Before(until) {
		r.mu.Unlock()
		return
	}
	// уже ждёт подтверждения — не дублируем
	if r.pending[symbol] {
		r.mu.Unlock()
		return
	}

	// попытка положить в очередь
	select {
	case r.queue <- signalReq{symbol: symbol, price: price, side: side}:
		r.pending[symbol] = true
		r.mu.Unlock()
	default:
		policy := r.cfg.ConfirmQueuePolicy
		r.mu.Unlock()

		switch policy {
		case "drop_oldest":
			select {
			case <-r.queue:
			default:
			}
			select {
			case r.queue <- signalReq{symbol, price, side}:
				r.setPending(symbol, true)
			default:
			}
		case "drop_same_symbol":
			return
		default:
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
			prompt := "🔔 [" + req.symbol + "] SIGNAL " + req.side +
				fmt.Sprintf(" @ %.4f\nSL/TP будут выставлены после входа. Войти?", req.price)

			ok := r.n.Confirm(ctx, prompt, r.cfg.ConfirmTimeout)
			if !ok {
				r.mu.Lock()
				r.cooldownTil[req.symbol] = time.Now().Add(r.cfg.CooldownPerSymbol)
				r.mu.Unlock()
				r.setPending(req.symbol, false)
				r.n.Sendf("⛔️ [%s] Вход отменён/таймаут", req.symbol)
				continue
			}

			// открываем реальный ордер на MEXC
			vol := 1.0
			sideInt := 1 // 1 = open long
			if req.side == "SELL" {
				sideInt = 3 // 3 = open short
			}
			openType := 1 // 1 = isolated

			orderID, err := r.mx.PlaceMarket(ctx, req.symbol, vol, sideInt, r.cfg.Leverage, openType)
			if err != nil {
				r.n.Sendf("❗️ [%s] Ошибка открытия ордера: %v", req.symbol, err)
				r.setPending(req.symbol, false)
				continue
			}

			r.n.Sendf(
				"✅ [%s] Вход подтверждён | OPEN %-4s @ %.4f | vol=%.4f lev=%dx | %s (orderId=%s)",
				req.symbol, req.side, req.price, vol, r.cfg.Leverage, r.stg.Dump(req.symbol), orderID,
			)
			r.setPending(req.symbol, false)
		}
	}
}
