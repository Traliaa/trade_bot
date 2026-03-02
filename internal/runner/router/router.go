package router

import (
	"context"
	"fmt"
	"sync"
	"time"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/repository/pg"
	"trade_bot/internal/modules/strategy/service"
	"trade_bot/internal/runner/sessions"

	"trade_bot/internal/models"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type TelegramNotifier interface {
	SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error)
	Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error)
	Confirm(ctx context.Context, chatID int64, prompt string, timeout time.Duration) bool
}

type UserSettingsSnapshot struct {
	UserID   int64
	Settings *models.UserSettings
}

// Router хранит активных юзеров и раздаёт сигналы.
type Router struct {
	mu               sync.RWMutex
	users            map[int64]*sessions.UserSession // userID -> сессия
	Repository       *pg.User
	TelegramNotifier TelegramNotifier
	config           *config.Config
	engine           *service.Service
}

func NewRouter(
	Repository *pg.User,
	TelegramNotifier TelegramNotifier,
	config *config.Config,
	engine *service.Service,
) *Router {
	return &Router{
		users:            make(map[int64]*sessions.UserSession),
		Repository:       Repository,
		TelegramNotifier: TelegramNotifier,
		config:           config,
		engine:           engine,
	}
}

func (r *Router) OnSignal(ctx context.Context, sig models.Signal) {

	r.mu.RLock()
	defer r.mu.RUnlock()

	fmt.Printf("[SIG ROUTER] %s %s",
		sig.TF, sig.Strategy)

	for _, sess := range r.users {
		select {
		case sess.Queue <- sig:
		default:
			// очередь забита — можно логнуть / дропнуть
		}
	}
}

// AutoTuneNow запускает тюн немедленно и возвращает результат для UI.
func (r *Router) AutoTuneNow(ctx context.Context) (models.TuneDecision, models.RuntimeTuning, time.Time, time.Time, bool, models.TuneMode) {
	eng := r.engine // твоя стратегия

	mode := r.engine.TuneMode() // или eng.TuneMode()
	dec := eng.AutoTuneNow()    // смотри примечание ниже

	cur, lastSignalAt, lastTuneAt := eng.CurrentTuning()
	warmupDone := eng.IsWarmupDone()

	return dec, cur, lastSignalAt, lastTuneAt, warmupDone, mode
}

func (r *Router) ToggleTuneMode(ctx context.Context) models.TuneMode {
	// если режим хранится в стратегии — просто toggle там
	newMode := r.engine.ToggleTuneMode()

	return newMode
}
func (r *Router) TuneMode(ctx context.Context) models.TuneMode {
	// если режим хранится в стратегии — просто toggle там
	newMode := r.engine.TuneMode()

	return newMode
}
