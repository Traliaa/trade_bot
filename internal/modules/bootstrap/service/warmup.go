package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okxws "trade_bot/internal/modules/okx_websocket/service"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/modules/telegram_public/public"
	"trade_bot/pkg/logger"

	"go.uber.org/zap"
)

type PublicNotifier interface {
	SendOrEdit(ctx context.Context, st public.Status) error
	SendServiceText(ctx context.Context, text string) (messageID int, err error)
}

type Warmuper struct {
	mx             *okxws.Client
	hub            *strategy.Hub
	publicNotifier PublicNotifier

	cfg *config.Config

	// ограничитель параллелизма, чтобы не словить rate limit
	sem chan struct{}
}

func NewWarmuper(mx *okxws.Client, hub *strategy.Hub, publicNotifier PublicNotifier, cfg *config.Config) *Warmuper {
	return &Warmuper{
		mx:             mx,
		hub:            hub,
		publicNotifier: publicNotifier,
		cfg:            cfg,
		sem:            make(chan struct{}, 8), // 8 параллельных символов
	}
}

func (w *Warmuper) Warmup(ctx context.Context, symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	total := len(symbols)

	// 1) Старт: понятное “мы подготавливаемся”
	_, err := w.publicNotifier.SendServiceText(ctx, public.Status{
		State:       public.StatePreparing,
		Exchange:    "OKX",
		Instruments: total,
		Progress:    0,
		UpdatedAt:   time.Now(),
	}.RenderHTML())

	var (
		wg       sync.WaitGroup
		firstErr error
		mu       sync.Mutex
	)

	// Прогресс (чтобы не спамить): обновляем максимум раз в N секунд
	var done int64
	progressTicker := time.NewTicker(4 * time.Second)
	defer progressTicker.Stop()

	// Отдельная горутина, которая иногда публикует прогресс
	stopProgress := make(chan struct{})
	defer close(stopProgress)

	go func() {
		for {
			select {
			case <-stopProgress:
				return
			case <-progressTicker.C:
				d := atomic.LoadInt64(&done)
				pct := int(d * 100 / int64(total))
				if pct < 0 {
					pct = 0
				}
				if pct > 99 { // 100 покажем финальным "готово"
					pct = 99
				}

				err = w.publicNotifier.SendOrEdit(ctx, public.Status{
					State:       public.StatePreparing,
					Exchange:    "OKX",
					Instruments: total,
					Progress:    pct,
					UpdatedAt:   time.Now(),
				})
				if err != nil {
					logger.Error("[Warmuper] warmup failed", zap.Error(err))
				}
			}
		}
	}()

	ltfNeed := w.cfg.Strategy.DonchianPeriod + 30
	htfNeed := w.cfg.Strategy.HTFEmaSlow + 30

	for _, sym := range symbols {
		sym := sym
		wg.Add(1)

		go func() {
			defer wg.Done()

			// ограничитель параллелизма
			w.sem <- struct{}{}
			defer func() { <-w.sem }()

			// 1) HTF (внутри — как было, это не в паблик)
			htf, err := w.mx.GetCandles(ctx, sym, w.cfg.Strategy.HTF, htfNeed)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("прогрев HTF %s: %w", sym, err)
				}
				mu.Unlock()
				return
			}
			for _, c := range htf {
				w.hub.OnTick(ctx, okxws.OutTick{
					InstID:    sym,
					Timeframe: w.cfg.Strategy.HTF,
					Candle: models.CandleTick{
						Open:  c.Open,
						High:  c.High,
						Low:   c.Low,
						Close: c.Close,
						Start: c.Start,
						End:   c.End,
					},
				})
			}

			// 2) LTF
			ltf, err := w.mx.GetCandles(ctx, sym, w.cfg.Strategy.LTF, ltfNeed)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("прогрев LTF %s: %w", sym, err)
				}
				mu.Unlock()
				return
			}
			for _, c := range ltf {
				w.hub.OnTick(ctx, okxws.OutTick{
					InstID:    sym,
					Timeframe: w.cfg.Strategy.LTF,
					Candle: models.CandleTick{
						Open:  c.Open,
						High:  c.High,
						Low:   c.Low,
						Close: c.Close,
						Start: c.Start,
						End:   c.End,
					},
				})
			}

			atomic.AddInt64(&done, 1)
		}()
	}

	wg.Wait()

	// Остановить прогресс-горутину (и избежать лишних апдейтов)
	// close(stopProgress) уже в defer выше — но wg.Wait() уже прошёл, можно просто выйти.

	if firstErr != nil {
		// 2) Ошибка: понятное человеку сообщение
		err = w.publicNotifier.SendOrEdit(ctx, public.Status{
			State:       public.StateError,
			Exchange:    "OKX",
			Instruments: total,
			UpdatedAt:   time.Now(),
		})
		if err != nil {
			return err
		}

		// (Опционально) подробности об ошибке — лучше в dev-лог, а не в публичный канал:
		// w.log.Error("warmup failed", zap.Error(firstErr))

		return firstErr
	}

	// 3) Готово: финальный статус
	_, err = w.publicNotifier.SendServiceText(ctx, public.Status{
		State:       public.StateReady,
		Exchange:    "OKX",
		Instruments: total,
		Progress:    100,
		UpdatedAt:   time.Now(),
	}.RenderHTML())
	if err != nil {
		return err
	}

	w.hub.SetWarmupDone()

	return nil
}
