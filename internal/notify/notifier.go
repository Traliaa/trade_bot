package notify

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"trade_bot/internal/exchange"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Notifier interface {
	Send(msg string)
	Sendf(format string, args ...any)
	Confirm(ctx context.Context, prompt string, timeout time.Duration) bool
}

// Telegram — пассивный нотифайер + обработка одной команды /positions.
type Telegram struct {
	bot    *tgbot.BotAPI
	chatID int64
	mx     *exchange.MexcClient

	mu       sync.Mutex
	pendings map[string]*pending
}

type pending struct {
	ch     chan bool
	msgID  int
	prompt string
}

func NewTelegram(token string, chatID int64, mx *exchange.MexcClient) (*Telegram, error) {
	b, err := tgbot.NewBotAPI(token)
	if err != nil {
		return nil, err
	}
	return &Telegram{
		bot:      b,
		chatID:   chatID,
		mx:       mx,
		pendings: make(map[string]*pending),
	}, nil
}

func (t *Telegram) Send(msg string) {
	if t == nil || t.bot == nil || t.chatID == 0 {
		return
	}
	_, _ = t.bot.Send(tgbot.NewMessage(t.chatID, msg))
}

func (t *Telegram) Sendf(format string, args ...any) { t.Send(fmt.Sprintf(format, args...)) }

// HandleCallback должен вызываться из Start() для callback_query.
func (t *Telegram) HandleCallback(cb *tgbot.CallbackQuery) {
	if t == nil || t.bot == nil || cb == nil {
		return
	}

	// ответ Telegram для остановки спиннера
	_, _ = t.bot.Request(tgbot.NewCallback(cb.ID, ""))

	data := cb.Data // ожидаем CONF::token / REJ::token
	var verb, token string
	for i := 0; i < len(data); i++ {
		if i+1 < len(data) && data[i] == ':' && data[i+1] == ':' {
			verb, token = data[:i], data[i+2:]
			break
		}
	}
	if verb == "" || token == "" {
		return
	}

	t.mu.Lock()
	p, ok := t.pendings[token]
	t.mu.Unlock()
	if !ok {
		return
	}

	accepted := verb == "CONF"
	p.ch <- accepted
	close(p.ch)

	status := "Отклонено"
	emoji := "❌"
	if accepted {
		status = "Подтверждено"
		emoji = "✅"
	}

	_ = t.editReplyMarkupRemove(t.chatID, p.msgID)
	_ = t.editText(t.chatID, p.msgID, fmt.Sprintf("%s\n\n%s %s", p.prompt, emoji, status))

	t.mu.Lock()
	delete(t.pendings, token)
	t.mu.Unlock()
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
func (t *Telegram) Confirm(ctx context.Context, prompt string, timeout time.Duration) bool {
	if t == nil || t.bot == nil || t.chatID == 0 {
		return true
	}

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

	msg := tgbot.NewMessage(t.chatID, prompt)
	msg.ReplyMarkup = kb

	sent, _ := t.bot.Send(msg)
	p.msgID = sent.MessageID

	tmr := time.NewTimer(timeout)
	defer tmr.Stop()

	select {
	case ok := <-p.ch:
		return ok
	case <-tmr.C:
		_ = t.editReplyMarkupRemove(t.chatID, p.msgID)
		_ = t.editText(t.chatID, p.msgID, fmt.Sprintf("%s\n\n⏳ Таймаут", prompt))
		t.mu.Lock()
		delete(t.pendings, token)
		t.mu.Unlock()
		return false
	case <-ctx.Done():
		_ = t.editReplyMarkupRemove(t.chatID, p.msgID)
		_ = t.editText(t.chatID, p.msgID, fmt.Sprintf("%s\n\n⛔️ Отменено", prompt))
		t.mu.Lock()
		delete(t.pendings, token)
		t.mu.Unlock()
		return false
	}
}

// /positions — вывод открытых позиций с MEXC
func (t *Telegram) handlePositions(ctx context.Context) {
	if t.mx == nil {
		t.Send("❗️ Клиент MEXC не инициализирован")
		return
	}
	positions, err := t.mx.OpenPositions(ctx)
	if err != nil {
		t.Sendf("❗️ Ошибка получения позиций: %v", err)
		return
	}
	if len(positions) == 0 {
		t.Send("📭 Открытых позиций нет")
		return
	}

	var b strings.Builder
	b.WriteString("📊 Открытые позиции:\n")
	for _, p := range positions {
		side := "LONG"
		if p.PositionType == 2 {
			side = "SHORT"
		}
		fmt.Fprintf(&b, "- %s [%s] vol=%.4f @ %.4f lev=%dx realised=%.4f\n",
			p.Symbol, side, p.HoldVol, p.HoldAvgPrice, p.Leverage, p.Realised)
	}
	t.Send(b.String())
}

// Start: long-polling для messages + callback_query.
func (t *Telegram) Start(ctx context.Context) error {
	if t == nil || t.bot == nil {
		return nil
	}

	u := tgbot.NewUpdate(0)
	u.Timeout = 30
	u.AllowedUpdates = []string{"message", "callback_query"}

	updates := t.bot.GetUpdatesChan(u)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case upd := <-updates:
				if upd.CallbackQuery != nil {
					t.HandleCallback(upd.CallbackQuery)
				}
				if upd.Message != nil && upd.Message.Chat != nil &&
					upd.Message.Chat.ID == t.chatID && upd.Message.IsCommand() {

					switch upd.Message.Command() {
					case "positions":
						go t.handlePositions(ctx)
					}
				}
			}
		}
	}()
	return nil
}

func (t *Telegram) Stop() {}

// Stdout — заглушка, всё логирует и всегда подтверждает.
type Stdout struct{}

func NewStdout() *Stdout                           { return &Stdout{} }
func (s *Stdout) Send(msg string)                  { log.Println(msg) }
func (s *Stdout) Sendf(format string, args ...any) { log.Printf(format, args...) }
func (s *Stdout) Confirm(ctx context.Context, prompt string, timeout time.Duration) bool {
	log.Printf("CONFIRM (auto-yes): %s", prompt)
	return true
}
