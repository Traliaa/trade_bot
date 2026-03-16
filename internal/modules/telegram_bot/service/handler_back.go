package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"trade_bot/internal/models"
	"trade_bot/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const cbAdminRejects = "ADMIN::REJECTS"
const cbAdminRejectsReset = "ADMIN::REJECTS_RESET"

func (t *Telegram) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	// 1) Обычные сообщения
	if msg := update.Message; msg != nil {
		chatID := msg.Chat.ID

		// Команды /start, /positions и т.п.
		if msg.IsCommand() {
			switch msg.Command() {
			case "start":
				if err := t.handleStart(ctx, chatID); err != nil {
					logger.Error("handleStart error: %v", err)
				}
			case "positions":
				go t.handlePositions(ctx) // если нужно, можешь прокинуть chatID
			default:
				// /help, /status и т.п. — по желанию
			}
			return
		}

		// Обычный текст (кнопки клавиатуры, OKX ключи и т.д.)
		t.handleTextMessage(ctx, msg)
		return
	}

	// 2) Inline-кнопки (CallbackQuery)
	if cb := update.CallbackQuery; cb != nil {
		// у callback всегда свой message
		if cb.Message == nil || cb.Message.Chat == nil {
			return
		}
		chatID := cb.Message.Chat.ID
		t.handleCallback(ctx, chatID, cb)
		return
	}

	// 3) Остальное (inline mode и т.п.) пока игнорируем
}
func (t *Telegram) handleStart(ctx context.Context, chatID int64) error {
	_, ok := t.router.GetSession(chatID)
	if !ok {
		_, err := t.Send(ctx, chatID, "Настройки не найдены, попробуй ещё раз /start")
		return err
	}

	// Главное меню
	replyKb := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("▶️ Запустить бота"),
			tgbotapi.NewKeyboardButton("⏹ Остановить бота"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("⚙️ Настройки"),
			tgbotapi.NewKeyboardButton("📊 Статус"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("📚 История"),
			tgbotapi.NewKeyboardButton("📈 Сделки"),
		),
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton("🧪 Тестовая сделка (BTC x1)"),
			tgbotapi.NewKeyboardButton("❓ Помощь"),
		),
	)

	msgText := "Привет! Я торговый бот для OKX.\n\n" +
		"1️⃣ Сначала укажи свои API-ключи OKX.\n" +
		"2️⃣ Затем можешь запустить бота кнопкой «▶️ Запустить бота».\n\n" +
		"Отправь свои API-ключи в формате:\n" +
		"`OKX: apiKey; apiSecret; passphrase`"

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = replyKb

	_, err := t.SendMessage(ctx, msg)
	return err
}
func (t *Telegram) handleTextMessage(_ context.Context, msg *tgbotapi.Message) {
	ctx := context.Background()
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	// 0) Если ждём ввод значения.
	if key, ok := t.peekAwait(chatID); ok {
		if strings.EqualFold(text, "отмена") {
			t.clearAwait(chatID)
			t.handleSettingsMenu(ctx, chatID)
			return
		}

		t.handleAwaitValue(ctx, chatID, text, key)
		return
	}

	// 1) Ключи OKX.
	if strings.HasPrefix(strings.ToUpper(text), "OKX:") {
		t.handleOkxKeys(ctx, msg)
		return
	}

	// 2) Получаем пользователя из БД, а не runtime session.
	user, err := t.router.GetUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	switch text {
	case "▶️ Запустить бота":
		user.Status = true

		// Если ApplySettings сохраняет и/или применяет настройки.
		t.router.ApplySettings(ctx, user)

		_, started := t.router.EnableUser(ctx, user)
		if started {
			_, _ = t.Send(ctx, chatID, "✅ Бот запущен для этого аккаунта.")
		} else {
			_, _ = t.Send(ctx, chatID, "ℹ️ Бот уже запущен.")
		}

		t.handleSettingsMenu(ctx, chatID)
		return

	case "⏹ Остановить бота":
		user.Status = false

		t.router.ApplySettings(ctx, user)

		stopped := t.router.DisableUser(ctx, chatID)
		if stopped {
			_, _ = t.Send(ctx, chatID, "🛑 Бот остановлен для этого аккаунта.")
		} else {
			_, _ = t.Send(ctx, chatID, "ℹ️ Бот уже остановлен.")
		}

		//t.handleSettingsMenu(ctx, chatID)
		return

	case "⚙️ Настройки":
		t.handleSettingsMenu(ctx, chatID)
		return

	case "📊 Статус":
		sess, ok := t.router.GetSession(chatID)
		if !ok {
			_, _ = t.Send(ctx, chatID, "⏸ Бот сейчас остановлен.")
			return
		}

		go t.handleStatus(ctx, sess.User)
		return
	case "/history", "📚 История":
		t.handleHistory(ctx, chatID)
		return
	case "/stats", "📈 Сделки":
		t.handleStats(ctx, chatID)
		return
	case "🧪 Тестовая сделка (BTC x1)":
		// Если тестовая сделка должна работать только при активном runtime.
		sess, ok := t.router.GetSession(chatID)
		if !ok {
			_, _ = t.Send(ctx, chatID, "⏸ Сначала запусти бота.")
			return
		}

		t.handleTestTradeMenu(ctx, chatID, sess.User)
		return

	case "❓ Помощь":
		t.handleHelp(ctx, chatID)
		return
	}
}

func (t *Telegram) handleOkxKeys(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)
	text = strings.TrimPrefix(text, "OKX:")
	text = strings.TrimPrefix(text, "okx:")
	text = strings.TrimSpace(text)

	parts := strings.Split(text, ";")
	if len(parts) != 3 {
		t.SendMessage(ctx, tgbotapi.NewMessage(chatID, "Формат: `OKX: apiKey; apiSecret; passphrase`"))
		return
	}

	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	session, ok := t.router.GetSession(chatID)
	if !ok {
		t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	session.User.Settings.TradingSettings.OKXAPIKey = parts[0]
	session.User.Settings.TradingSettings.OKXAPISecret = parts[1]
	session.User.Settings.TradingSettings.OKXPassphrase = parts[2]

	t.router.ApplySettings(ctx, session.User) // ✅ горячее применение
	t.handleSettingsMenu(ctx, chatID)

	t.bot.Send(tgbotapi.NewMessage(chatID, "✅ Ключи OKX сохранены. Теперь можно запускать торговлю."))
}

func (t *Telegram) handleSetTimeframe(ctx context.Context, chatID int64, msg *tgbotapi.Message) {
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1m", "tf_1m"),
			tgbotapi.NewInlineKeyboardButtonData("5m", "tf_5m"),
			tgbotapi.NewInlineKeyboardButtonData("15m", "tf_15m"),
		),
	)
	out := tgbotapi.NewMessage(chatID, "Выбери таймфрейм:")
	out.ReplyMarkup = kb
	_, _ = t.SendMessage(ctx, out)
}

func (t *Telegram) handleStatus(ctx context.Context, user *models.UserSettings) {
	positions, err := t.router.StatusForUser(ctx, user.TelegramID)
	if err != nil {
		log.Printf("StatusForUser error: %v", err)
		_, _ = t.Send(ctx, user.TelegramID, "⚠️ Не удалось получить позиции с OKX: "+err.Error())
		return
	}

	if len(positions) == 0 {
		msg := tgbotapi.NewMessage(user.TelegramID, "📊 Открытых позиций нет.")
		msg.ParseMode = "Markdown"
		_, _ = t.SendMessage(ctx, msg)
		return
	}

	// helpers
	fixNegZero := func(v float64) float64 {
		if math.Abs(v) < 0.0000005 {
			return 0
		}
		return v
	}
	priceDecimals := func(px float64) int {
		px = math.Abs(px)
		switch {
		case px == 0:
			return 6
		case px >= 1000:
			return 2
		case px >= 1:
			return 4
		case px >= 0.01:
			return 6
		default:
			return 8 // SHIB/PEPE
		}
	}
	fmtPrice := func(px float64) string {
		if px == 0 {
			return "—"
		}
		return fmt.Sprintf("%.*f", priceDecimals(px), px)
	}
	fmtQty := func(q float64) string {
		aq := math.Abs(q)
		switch {
		case aq >= 100:
			return fmt.Sprintf("%.2f", q)
		case aq >= 1:
			return fmt.Sprintf("%.4f", q)
		default:
			return fmt.Sprintf("%.6f", q)
		}
	}
	fmtMoney := func(v float64) string {
		v = fixNegZero(v)
		av := math.Abs(v)
		switch {
		case av >= 10:
			return fmt.Sprintf("%.2f", v)
		case av >= 1:
			return fmt.Sprintf("%.3f", v)
		default:
			return fmt.Sprintf("%.5f", v)
		}
	}
	sideTitle := func(side string) (emoji, title string) {
		s := strings.ToLower(strings.TrimSpace(side))
		switch s {
		case "long":
			return "🟢", "LONG"
		case "short":
			return "🔴", "SHORT"
		default:
			return "⚪️", strings.ToUpper(side)
		}
	}

	var b strings.Builder
	b.WriteString("📊 *Открытые позиции*\n\n")

	var totalPnl float64

	for _, p := range positions {
		symbol := p.Symbol
		emo, side := sideTitle(p.Side)

		qty := p.Size
		entry := p.EntryPrice
		last := p.LastPrice
		upnl := fixNegZero(p.UnrealizedPnL)
		upnlPct := fixNegZero(p.UnrealizedPct)

		totalPnl += upnl

		arrow := "➡️"
		if upnl > 0 {
			arrow = "📈"
		} else if upnl < 0 {
			arrow = "📉"
		}

		fmt.Fprintf(&b,
			"%s *%s*  `%s`\n"+
				"• Плечо: `%dx`\n"+
				"• Размер: `%s`\n"+
				"• Вход: `%s`\n"+
				"• Сейчас: `%s`\n"+
				"• PnL: %s `%s USDT` (`%+.2f%%`)\n\n",
			emo, symbol, side,
			p.Leverage,
			fmtQty(qty),
			fmtPrice(entry),
			fmtPrice(last),
			arrow, fmtMoney(upnl), upnlPct,
		)
	}

	totalPnl = fixNegZero(totalPnl)
	totalMark := "➡️"
	if totalPnl > 0 {
		totalMark = "🟩"
	} else if totalPnl < 0 {
		totalMark = "🟥"
	}

	fmt.Fprintf(&b, "%s *Суммарный PnL:* `%s USDT`\n", totalMark, fmtMoney(totalPnl))

	msg := tgbotapi.NewMessage(user.TelegramID, b.String())
	msg.ParseMode = "Markdown"
	_, _ = t.SendMessage(ctx, msg)
}
