package service

import (
	"context"
	"fmt"
	"sync"
	"time"
	"trade_bot/internal/models"
	"trade_bot/internal/modules/config"
	"trade_bot/internal/modules/runner/sessions"
	"trade_bot/pkg/logger"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type pending struct {
	ch     chan bool
	msgID  int
	prompt string
}

// Telegram
type Telegram struct {
	bot      *tgbot.BotAPI
	cfg      *config.Config
	mu       sync.Mutex
	pendings map[string]*pending
	router   Router
	await    *awaitStore
}

type Router interface {
	DisableUser(ctx context.Context, userID int64) bool
	EnableUser(ctx context.Context, user *models.UserSettings) (*sessions.UserSession, bool)
	ApplySettings(ctx context.Context, user *models.UserSettings)
	StatusForUser(ctx context.Context, userID int64) ([]models.OpenPosition, error)
	GetSession(userID int64) (*sessions.UserSession, bool)
	AutoTuneNow(ctx context.Context) (models.TuneDecision, models.RuntimeTuning, time.Time, time.Time, bool, models.TuneMode)
	ToggleTuneMode(ctx context.Context) models.TuneMode
	TuneMode(ctx context.Context) models.TuneMode
	StrategyRejects(reset bool) models.RejectSnapshot
	StrategyTuning() (models.RuntimeTuning, time.Time, time.Time)
	GetUser(ctx context.Context, userID int64) (*models.UserSettings, error)
	ListRecentTrades(ctx context.Context, userID int64, limit int) ([]models.TradeRecord, error)
	GetTradeStats(ctx context.Context, userID int64) (models.TradeStats, error)
}

func (t *Telegram) SetRouter(r Router) {
	t.router = r
}
func NewTelegram(cfg *config.Config) (*Telegram, error) {
	b, err := tgbot.NewBotAPI(cfg.Telegram.Token)
	if err != nil {
		return nil, err
	}

	return &Telegram{
		bot:      b,
		cfg:      cfg,
		pendings: make(map[string]*pending),

		await: newAwaitStore(),
	}, nil
}

func (t *Telegram) ask(ctx context.Context, chatID int64, prompt string, awaitKey string) {
	// гарантируем что awaitStore есть даже если кто-то забыл инициализировать
	if t.await == nil {
		t.await = newAwaitStore()
	}

	t.setAwait(chatID, awaitKey)
	_, _ = t.Send(ctx, chatID, prompt)
}

func (t *Telegram) SendService(ctx context.Context, format string, args ...any) {
	if t.cfg.ServiceTelegramChatID == 0 {
		return
	}

	// ✅ Если аргументов нет — это НЕ формат, это готовый текст.
	text := format
	if len(args) > 0 {
		text = fmt.Sprintf(format, args...)
	}

	_, err := t.Send(ctx, int64(t.cfg.ServiceTelegramChatID), text)
	if err != nil {
		logger.Error(err.Error())
	}
}

func (t *Telegram) Send(ctx context.Context, chatID int64, msg string) (tgbot.Message, error) {
	return t.bot.Send(tgbot.NewMessage(chatID, msg))
}

func (t *Telegram) SendF(ctx context.Context, chatID int64, format string, args ...any) (tgbot.Message, error) {
	return t.Send(ctx, chatID, fmt.Sprintf(format, args...))
}

func (t *Telegram) SendMessage(_ context.Context, message tgbot.MessageConfig) (tgbot.Message, error) {
	return t.bot.Send(message)
}

func (t *Telegram) SendMessageToChannel(_ context.Context, username string, text string) (tgbot.Message, error) {
	return t.bot.Send(tgbot.NewMessageToChannel(username, text))
}

func (t *Telegram) editReplyMarkupRemove(chatID int64, msgID int) error {
	rm := tgbot.InlineKeyboardMarkup{InlineKeyboard: [][]tgbot.InlineKeyboardButton{}}
	edit := tgbot.NewEditMessageReplyMarkup(chatID, msgID, rm)
	_, err := t.bot.Request(edit)
	return err
}

func (t *Telegram) editText(chatID int64, msgID int, text string) error {
	edit := tgbot.NewEditMessageText(chatID, msgID, text)
	_, err := t.bot.Request(edit)
	return err
}

// Confirm — сообщение с кнопками и ожиданием callback.
func (t *Telegram) Confirm(ctx context.Context, chatID int64, prompt string, timeout time.Duration) bool {

	token := fmt.Sprintf("%d", time.Now().UnixNano())
	p := &pending{
		ch:     make(chan bool, 1),
		prompt: prompt,
	}

	t.mu.Lock()
	t.pendings[token] = p
	t.mu.Unlock()

	btnYes := tgbot.NewInlineKeyboardButtonData("✅ Войти", "CONF::"+token)
	btnNo := tgbot.NewInlineKeyboardButtonData("❌ Пропустить", "REJ::"+token)
	kb := tgbot.NewInlineKeyboardMarkup(tgbot.NewInlineKeyboardRow(btnYes, btnNo))

	msg := tgbot.NewMessage(chatID, prompt)
	msg.ReplyMarkup = kb

	sent, _ := t.bot.Send(msg)
	p.msgID = sent.MessageID

	tmr := time.NewTimer(timeout)
	defer tmr.Stop()

	select {
	case ok := <-p.ch:
		return ok
	case <-tmr.C:
		_ = t.editReplyMarkupRemove(chatID, p.msgID)
		_ = t.editText(chatID, p.msgID, fmt.Sprintf("%s\n\n⏳ Таймаут", prompt))
		t.mu.Lock()
		delete(t.pendings, token)
		t.mu.Unlock()
		return false
	case <-ctx.Done():
		_ = t.editReplyMarkupRemove(chatID, p.msgID)
		_ = t.editText(chatID, p.msgID, fmt.Sprintf("%s\n\n⛔️ Отменено", prompt))
		t.mu.Lock()
		delete(t.pendings, token)
		t.mu.Unlock()
		return false
	}
}

// /positions — вывод открытых позиций с MEXC
func (t *Telegram) handlePositions(ctx context.Context) {
	//if t.mx == nil {
	//	t.Send("❗️ Клиент MEXC не инициализирован")
	//	return
	//}
	//positions, err := t.mx.OpenPositions(ctx)
	//if err != nil {
	//	t.Sendf("❗️ Ошибка получения позиций: %v", err)
	//	return
	//}
	//if len(positions) == 0 {
	//	t.Send("📭 Открытых позиций нет")
	//	return
	//}
	//
	//var b strings.Builder
	//b.WriteString("📊 Открытые позиции:\n")
	//for _, p := range positions {
	//	side := "LONG"
	//	if p.PositionType == 2 {
	//		side = "SHORT"
	//	}
	//	fmt.Fprintf(&b, "- %s [%s] vol=%.4f @ %.4f lev=%dx realised=%.4f\n",
	//		p.Symbol, side, p.HoldVol, p.HoldAvgPrice, p.Leverage, p.Realised)
	//}
	t.Send(ctx, 0, "b.String()")
}

// Start ...
func (t *Telegram) Start(ctx context.Context) error {
	u := tgbot.NewUpdate(0)
	u.Timeout = 30
	updates := t.bot.GetUpdatesChan(u)
	for update := range updates {
		t.handleUpdate(ctx, update)
	}
	return nil
}

func (t *Telegram) Stop() {}
