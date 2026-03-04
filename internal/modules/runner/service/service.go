package service

import (
	"context"
	"sync"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/repository/pg"
	"trade_bot/internal/modules/runner/sessions"
	strategy "trade_bot/internal/modules/strategy/service"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Params - parameters.
type Params struct {
	fx.In
	Config *ModuleConfig
}
type TelegramNotifier interface {
	SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error)
	Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error)
	Confirm(ctx context.Context, chatID int64, prompt string, timeout time.Duration) bool
}

type UserSettingsSnapshot struct {
	UserID   int64
	Settings *models.UserSettings
}

// Service хранит активных юзеров и раздаёт сигналы.
type Service struct {
	base.Base

	mu               sync.RWMutex
	users            map[int64]*sessions.UserSession // userID -> сессия
	Repository       *pg.User
	TelegramNotifier TelegramNotifier
	config           *config.Config
	strategy         *strategy.Service
}

func NewService(
	params Params,
	Repository *pg.User,
	TelegramNotifier TelegramNotifier,
	strategy *strategy.Service,
) *Service {
	return &Service{
		users:            make(map[int64]*sessions.UserSession),
		Repository:       Repository,
		TelegramNotifier: TelegramNotifier,
		config:           params.Config.cfg,
		strategy:         strategy,
	}
}

// Start ...
func (s *Service) Start(ctx context.Context, sigs chan models.Signal, candles chan models.CandleTick) error {
	ctx, shouldStart, started, stopped := s.StartInit(ctx)
	if !shouldStart {
		return nil
	}

	go func() {
		started()
		defer stopped()

		s.Logger.Debug("Цикл запуска начат")
		defer s.Logger.Debug("Цикл запуска остановлен")

		s.RestoreEnabled(ctx)

		agg := NewCandleAgg()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			// 1) ingest 1m candles
			case ct, ok := <-candles:
				if !ok {
					return
				}
				if helper.NormTF(ct.TimeframeRaw) == "1m" {
					agg.Put(ct)
				}
				// 2) route signals
			case sig, ok := <-sigs:
				if !ok {
					return
				}
				s.OnSignal(ctx, sig)
				// 3) periodic drain
			case <-ticker.C:
				batch := agg.Drain()
				for _, ct := range batch {
					s.OnCandleClose(ctx, ct)
				}
			}
		}
	}()
	return nil
}

func (r *Service) OnSignal(ctx context.Context, sig models.Signal) {

	r.mu.RLock()
	defer r.mu.RUnlock()
	r.Logger.Debug("okx ws publish candle",
		zap.String("instId", sig.InstID),
		zap.String("tf", sig.TF),
	)

	for _, sess := range r.users {
		select {
		case sess.Queue <- sig:
		default:
			// очередь забита — можно логнуть / дропнуть
		}
	}
}

// AutoTuneNow запускает тюн немедленно и возвращает результат для UI.
func (r *Service) AutoTuneNow(ctx context.Context) (models.TuneDecision, models.RuntimeTuning, time.Time, time.Time, bool, models.TuneMode) {
	mode := r.strategy.TuneMode()   // или eng.TuneMode()
	dec := r.strategy.AutoTuneNow() // смотри примечание ниже

	cur, lastSignalAt, lastTuneAt := r.strategy.CurrentTuning()
	warmupDone := r.strategy.IsWarmupDone()

	return dec, cur, lastSignalAt, lastTuneAt, warmupDone, mode
}

func (r *Service) ToggleTuneMode(ctx context.Context) models.TuneMode {
	// если режим хранится в стратегии — просто toggle там
	newMode := r.strategy.ToggleTuneMode()

	return newMode
}
func (s *Service) TuneMode(ctx context.Context) models.TuneMode {
	// если режим хранится в стратегии — просто toggle там
	newMode := s.strategy.TuneMode()

	return newMode
}

func (r *Service) StrategyRejects(reset bool) models.RejectSnapshot {
	return r.strategy.RejectSnapshot(reset)
}
func (r *Service) StrategyTuning() (models.RuntimeTuning, time.Time, time.Time) {
	return r.strategy.CurrentTuning()
}
