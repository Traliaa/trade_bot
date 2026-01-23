package service

import (
	"context"
	"fmt"
	"sync"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	okxws "trade_bot/internal/modules/okx_websocket/service"
	strategy "trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/modules/telegram_bot/service"
)

type Warmuper struct {
	mx  *okxws.Client
	hub *strategy.Hub
	n   *service.Telegram

	cfg *config.Config

	// ограничитель параллелизма, чтобы не словить rate limit
	sem chan struct{}
}

func NewWarmuper(mx *okxws.Client, hub *strategy.Hub, n *service.Telegram, cfg *config.Config) *Warmuper {
	return &Warmuper{
		mx:  mx,
		hub: hub,
		n:   n,
		cfg: cfg,
		sem: make(chan struct{}, 8), // 8 параллельных символов
	}
}

func (w *Warmuper) Warmup(ctx context.Context, symbols []string) error {
	if len(symbols) == 0 {
		return nil
	}

	ltfNeed := w.cfg.Strategy.DonchianPeriod + 30
	htfNeed := w.cfg.Strategy.HTFEmaSlow + 30

	// Публичное сообщение в канал (на русском)
	w.n.SendService(ctx, fmt.Sprintf(
		"🔥 Прогрев данных (REST) запущен\n\n"+
			"• Инструментов: %d\n"+
			"• Младший ТФ (LTF): %s — нужно %d свечей\n"+
			"• Старший ТФ (HTF): %s — нужно %d свечей",
		len(symbols), w.cfg.Strategy.LTF, ltfNeed, w.cfg.Strategy.HTF, htfNeed,
	))

	var wg sync.WaitGroup
	var firstErr error
	var mu sync.Mutex

	for _, sym := range symbols {
		sym := sym
		wg.Add(1)

		go func() {
			defer wg.Done()

			// ограничитель параллелизма
			w.sem <- struct{}{}
			defer func() { <-w.sem }()

			// 1) HTF
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
		}()
	}

	wg.Wait()

	if firstErr != nil {
		// Публичное сообщение в канал (на русском)
		w.n.SendService(ctx,
			"⚠️ *Прогрев данных завершён с ошибкой*\n\n"+
				"Причина: "+firstErr.Error()+"\n\n"+
				"👉 Если вы пользователь бота: откройте бота и нажмите *▶️ Запустить бота*.",
		)
		return firstErr
	}

	// Публичное сообщение в канал (на русском)
	w.n.SendService(ctx,
		"✅ *Прогрев данных завершён*\n\n"+
			"Бот готов работать в реальном времени (WebSocket).",
	)
	return nil
}
