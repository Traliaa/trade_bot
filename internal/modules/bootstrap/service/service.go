package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okxws "trade_bot/internal/modules/okx_websocket/service"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/modules/telegram_public/public"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Params - parameters.
type Params struct {
	fx.In
	Config *ModuleConfig
}

type PublicNotifier interface {
	SendOrEdit(ctx context.Context, st public.Status) error
	SendServiceText(ctx context.Context, text string) (messageID int, err error)
}

type Service struct {
	base.Base
	mx             *okxws.Service
	hub            *strategy.Service
	publicNotifier PublicNotifier

	cfg *config.Config

	// ограничитель параллелизма, чтобы не словить rate limit
	sem     chan struct{}
	started atomic.Bool
	done    atomic.Bool
}

func NewService(params Params, mx *okxws.Service, hub *strategy.Service, publicNotifier PublicNotifier) *Service {

	return &Service{
		mx:             mx,
		hub:            hub,
		publicNotifier: publicNotifier,
		cfg:            params.Config.cfg,
		sem:            make(chan struct{}, 8), // 8 параллельных символов
	}
}

func (s *Service) Start(ctx context.Context) error {
	ctx, shouldStart, started, stopped := s.StartInit(ctx)
	if !shouldStart {
		return nil
	}

	go func() {
		started()
		defer stopped()

		s.Logger.Info("strategy loop started")
		defer s.Logger.Info("strategy loop stopped", zap.Error(context.Cause(ctx)))

		// ✅ если уже прогреты — не делаем ничего
		if s.hub.IsWarmupDone() {
			s.Logger.Info("[BOOT] warmup already done, skip")
			return
		}

		// ✅ ждём пока Symbols заполнится до WatchTopN
		if err := s.waitUntilSymbolsReady(ctx, 250*time.Millisecond); err != nil {
			s.Logger.Error("[BOOT] wait symbols failed", zap.Error(err))
			return
		}

		if err := s.Warmup(ctx, s.cfg.Strategy.Symbols); err != nil {
			s.Logger.Info("[BOOT] warmup error: %v", zap.Error(err))
			return
		}
		s.Logger.Info("[BOOT] warmup done: %d symbols", zap.Strings("sym", s.cfg.Strategy.Symbols))

	}()

	return nil
}

func (s *Service) waitUntilSymbolsReady(ctx context.Context, poll time.Duration) error {
	t := time.NewTicker(poll)
	defer t.Stop()

	for {
		if len(s.cfg.Strategy.Symbols) >= s.cfg.Strategy.WatchTopN && s.cfg.Strategy.WatchTopN > 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-t.C:
		}
	}
}

func (s *Service) Warmup(ctx context.Context, symbols []string) error {
	if s.done.Load() {
		s.Logger.Info("skip: already done")
		return nil
	}
	if !s.started.CompareAndSwap(false, true) {
		s.Logger.Info("skip: already running")
		return nil
	}
	defer func() {
		// если упали с ошибкой — разрешим повтор
		if !s.done.Load() {
			s.started.Store(false)
		}
	}()

	if len(symbols) == 0 {
		return nil
	}
	ltfTF := s.hub.LTF()
	htfTF := s.hub.HTF()

	ltfNeed := s.hub.LTFNeed()
	htfNeed := s.hub.HTFNeed()
	total := len(symbols)

	// 1) Старт: понятное “мы подготавливаемся”
	_, err := s.publicNotifier.SendServiceText(ctx, public.Status{
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

				err = s.publicNotifier.SendOrEdit(ctx, public.Status{
					State:       public.StatePreparing,
					Exchange:    "OKX",
					Instruments: total,
					Progress:    pct,
					UpdatedAt:   time.Now(),
				})
				if err != nil {
					s.Logger.Error("warmup failed", zap.Error(err))
				}
			}
		}
	}()

	for _, sym := range symbols {
		sym := sym
		wg.Add(1)

		go func() {
			defer wg.Done()

			// ограничитель параллелизма
			s.sem <- struct{}{}
			defer func() { <-s.sem }()

			// 1) HTF (внутри — как было, это не в паблик)
			htf, err := s.mx.GetCandles(ctx, sym, htfTF, htfNeed)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("прогрев HTF %s: %s", sym, err)
				}
				mu.Unlock()
				return
			}
			for _, c := range htf {
				s.hub.OnTick(ctx, models.CandleTick{
					Open:         c.Open,
					High:         c.High,
					Low:          c.Low,
					Close:        c.Close,
					Start:        c.Start,
					End:          c.End,
					TimeframeRaw: htfTF,
					InstID:       sym,
				})
			}

			// 2) LTF
			ltf, err := s.mx.GetCandles(ctx, sym, ltfTF, ltfNeed)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("прогрев LTF %s: %s", sym, err)
				}
				mu.Unlock()
				return
			}
			for _, c := range ltf {
				s.hub.OnTick(ctx, models.CandleTick{
					Open:         c.Open,
					High:         c.High,
					Low:          c.Low,
					Close:        c.Close,
					Start:        c.Start,
					End:          c.End,
					TimeframeRaw: ltfTF,
					InstID:       sym,
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
		err = s.publicNotifier.SendOrEdit(ctx, public.Status{
			State:       public.StateError,
			Exchange:    "OKX",
			Instruments: total,
			UpdatedAt:   time.Now(),
		})
		if err != nil {
			return err
		}

		// (Опционально) подробности об ошибке — лучше в dev-лог, а не в публичный канал:
		// s.log.Error("warmup failed", zap.Error(firstErr))

		return firstErr
	}

	// 3) Готово: финальный статус
	err = s.publicNotifier.SendOrEdit(ctx, public.Status{
		State:       public.StateReady,
		Exchange:    "OKX",
		Instruments: total,
		Progress:    100,
		UpdatedAt:   time.Now(),
	})
	if err != nil {
		return err
	}

	s.hub.SetWarmupDone()
	s.done.Store(true)

	return nil
}
