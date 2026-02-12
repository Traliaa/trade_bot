package router

import (
	"context"
	"fmt"
	"sync"
	"time"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/repository/pg"
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
}

func NewRouter(
	Repository *pg.User,
	TelegramNotifier TelegramNotifier,
	config *config.Config,
) *Router {
	return &Router{
		users:            make(map[int64]*sessions.UserSession),
		Repository:       Repository,
		TelegramNotifier: TelegramNotifier,
		config:           config,
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
