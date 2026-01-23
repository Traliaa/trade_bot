package runner

import (
	"context"
	"sync"
	"time"
	"trade_bot/internal/models"
	okx_client "trade_bot/internal/modules/okx_client/service"
	okx_websocket "trade_bot/internal/modules/okx_websocket/service"
	service2 "trade_bot/internal/modules/strategy/service"

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

	// deps
	mkt *okx_websocket.Client
	cfg *models.UserSettings
	mx  *okx_client.Client
	stg service2.Engine
	n   TelegramNotifier

	// runtime
	queue       chan models.Signal
	pending     map[string]bool
	cooldownTil map[string]time.Time
	lastTick    map[string]time.Time

	mu       sync.Mutex
	healthMu sync.Mutex
}

func New(user *models.UserSettings, n TelegramNotifier, mkt *okx_websocket.Client, stg service2.Engine) *Runner {
	if user == nil {
		panic("runner.New: user is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Runner{
		ctx:    ctx,
		cancel: cancel,

		cfg: user,
		mx:  okx_client.NewClient(user),
		n:   n,
		stg: stg,
		mkt: mkt,

		queue:       make(chan models.Signal, 32),
		pending:     make(map[string]bool),
		cooldownTil: make(map[string]time.Time),
		lastTick:    make(map[string]time.Time),
	}
}

func (r *Runner) Ctx() context.Context { return r.ctx }

func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}
