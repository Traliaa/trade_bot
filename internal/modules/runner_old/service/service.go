package service

import (
	"context"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/base"
	"trade_bot/internal/helper"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/repository/pg"
	"trade_bot/internal/modules/runner_old/sessions"
	strategy "trade_bot/internal/modules/strategy/service"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/google/uuid"
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
func (r *Service) Start(ctx context.Context, sigs chan models.Signal, candles chan models.CandleTick) error {
	ctx, shouldStart, started, stopped := r.StartInit(ctx)
	if !shouldStart {
		return nil
	}

	go func() {
		started()
		defer stopped()

		r.Logger.Debug("Цикл запуска начат")
		defer r.Logger.Debug("Цикл запуска остановлен")

		r.RestoreEnabled(ctx)

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
				r.OnSignal(ctx, sig)
				// 3) periodic drain
			case <-ticker.C:
				batch := agg.Drain()
				for _, ct := range batch {
					r.OnCandleClose(ctx, ct)
				}
			}
		}
	}()
	return nil
}

func (r *Service) OnSignal(ctx context.Context, sig models.Signal) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	r.Logger.Info("signal",
		zap.String("instId", sig.InstID),
		zap.String("tf", sig.TF),
		zap.String("side", string(sig.Side)),
		zap.Float64("price", sig.Price),
	)

	for _, sess := range r.users {
		// 1. лимит по открытым позициям
		if sess.User.Settings.TradingSettings.MaxOpenPositions > 0 {
			if len(sess.TrailStates) >= sess.User.Settings.TradingSettings.MaxOpenPositions {
				sess.Notifier.SendF(ctx, sess.User.TelegramID,
					"⚠️ [%s] Лимит открытых позиций (%d) достигнут, сигнал пропущен",
					sig.InstID, sess.User.Settings.TradingSettings.MaxOpenPositions,
				)
				continue
			}
		}

		// 1.1 лимит по стороне
		sess.TrailMu.RLock()
		longs, shorts := countOpenSides(sess.TrailStates)
		sess.TrailMu.RUnlock()

		side := strings.ToLower(string(sig.Side))
		isLong := side == "buy" || side == "long"
		isShort := side == "sell" || side == "short"

		if isLong &&
			sess.User.Settings.TradingSettings.MaxLongPositions > 0 &&
			longs >= sess.User.Settings.TradingSettings.MaxLongPositions {

			sess.Notifier.SendF(ctx, sess.User.TelegramID,
				"⚠️ [%s] Лимит LONG позиций (%d) достигнут, сигнал пропущен",
				sig.InstID,
				sess.User.Settings.TradingSettings.MaxLongPositions,
			)
			continue
		}

		if isShort &&
			sess.User.Settings.TradingSettings.MaxShortPositions > 0 &&
			shorts >= sess.User.Settings.TradingSettings.MaxShortPositions {

			sess.Notifier.SendF(ctx, sess.User.TelegramID,
				"⚠️ [%s] Лимит SHORT позиций (%d) достигнут, сигнал пропущен",
				sig.InstID,
				sess.User.Settings.TradingSettings.MaxShortPositions,
			)
			continue
		}

		// 2. расчёт параметров сделки
		params, err := sess.CalcTradeParams(ctx, sig.InstID, string(sig.Side), sig.Price)
		if err != nil {
			sess.Notifier.SendF(ctx, sess.User.TelegramID,
				"❗️ [%s] Ошибка расчёта параметров сделки: %v",
				sig.InstID, err,
			)
			continue
		}

		// 3. открытие позиции
		res, err := sess.OpenPositionWithTpSl(ctx, sig, params)
		if err != nil {
			sess.Notifier.SendF(ctx, sess.User.TelegramID,
				"❗️ [%s] Ошибка открытия ордера: %v",
				sig.InstID, err,
			)
			continue
		}

		now := time.Now().UTC()

		trade := models.TradeRecord{
			GUID:        uuid.New(),
			UserID:      sess.User.TelegramID,
			InstID:      sig.InstID,
			Strategy:    string(sig.Strategy),
			Timeframe:   sig.TF,
			Status:      models.TradeStatusOpen,
			CloseReason: models.CloseReasonUnknown,
			EntryAt:     now,
			CreatedAt:   now,
			UpdatedAt:   now,
			Payload: models.TradePayload{
				PosSide:     res.PosSide,
				Side:        side,
				EntryPrice:  res.Entry,
				EntrySize:   params.Size,
				StopLoss:    params.SL,
				TakeProfit:  params.TP,
				Leverage:    int64(sess.User.Settings.TradingSettings.Leverage),
				OpenOrderID: res.OrderID,
				AlgoID:      res.SLAlgoID,
				RiskDist:    params.RiskDist,
			},
		}

		if err := r.Repository.CreateTradeHistory(ctx, trade); err != nil {
			r.Logger.Error("create trade history failed",
				zap.Error(err),
				zap.String("instId", sig.InstID),
				zap.Int64("userID", sess.User.TelegramID),
			)
		}

		// 4. уведомление об успешном открытии
		msg := formatOpenPositionMessage(sig, res, params)
		sess.Notifier.Send(ctx, sess.User.TelegramID, msg)

		// 5. сохраняем трейл-состояние
		if res.SLAlgoID == "" {
			continue
		}

		key := models.PosKey{sig.InstID, res.PosSide}

		sess.TrailMu.Lock()
		if sess.TrailStates == nil {
			sess.TrailStates = make(map[models.PosKey]*models.PositionTrailState)
		}
		sess.TrailStates[key] = &models.PositionTrailState{
			InstID:   sig.InstID,
			PosSide:  res.PosSide,
			Entry:    res.Entry,
			SL:       params.SL,
			TP:       params.TP,
			RiskDist: params.RiskDist,
			TickSz:   params.TickSize,
			AlgoID:   res.SLAlgoID,
			Size:     params.Size,
			MFE:      res.Entry,
			OpenedAt: now,
		}
		sess.TrailMu.Unlock()
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
func (r *Service) TuneMode(ctx context.Context) models.TuneMode {
	// если режим хранится в стратегии — просто toggle там
	newMode := r.strategy.TuneMode()

	return newMode
}

func (r *Service) StrategyRejects(reset bool) models.RejectSnapshot {
	return r.strategy.RejectSnapshot(reset)
}
func (r *Service) StrategyTuning() (models.RuntimeTuning, time.Time, time.Time) {
	return r.strategy.CurrentTuning()
}
func (r *Service) ListRecentTrades(ctx context.Context, userID int64, limit int) ([]models.TradeRecord, error) {
	return r.Repository.ListRecentTrades(ctx, userID, int32(limit))
}
func (r *Service) ListOpenTrades(ctx context.Context, userID int64) ([]models.TradeRecord, error) {
	return r.Repository.ListOpenTrades(ctx, userID)
}

func (r *Service) GetUserStatus(ctx context.Context, userID int64) (models.UserStatus, error) {
	openTrades, err := r.Repository.ListOpenTrades(ctx, userID)
	if err != nil {
		return models.UserStatus{}, err
	}

	status := models.UserStatus{
		BotRunning: false,
		Account:    models.AccountSnapshot{},
		OpenTrades: models.NewTradeListItems(openTrades),
	}

	r.mu.RLock()
	sess, ok := r.users[userID]
	r.mu.RUnlock()

	if ok && sess != nil {
		status.BotRunning = true
		status.Account = sess.AccountSnapshot()
	}

	return status, nil
}
func (r *Service) GetTradeStats(ctx context.Context, userID int64) (models.TradeStats, error) {
	return r.Repository.GetTradeStats(ctx, userID)
}
