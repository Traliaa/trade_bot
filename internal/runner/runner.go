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

	mkt *okx_websocket.Client
	cfg *models.UserSettings
	mx  *okx_client.Client
	stg service2.Engine
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
		cfg: user,
		mx:  okx_client.NewClient(user),
		n:   n,
		//stg:         service2.NewEngine(),
		queue:       make(chan models.Signal, qsize),
		pending:     make(map[string]bool),
		cooldownTil: make(map[string]time.Time),
		lastTick:    make(map[string]time.Time),
		mkt:         mkt,
	}
}

// Stop — мягко гасит раннер.
func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}
