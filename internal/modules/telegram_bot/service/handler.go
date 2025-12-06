package service

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"trade_bot/internal/models"
	"trade_bot/pkg/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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
	_, err := t.getUser(ctx, chatID)
	if err != nil {
		_, err = t.Send(ctx, chatID, "Настройки не найдены, попробуй ещё раз /start")
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
	)

	msgText := "Привет! Я торговый бот для OKX.\n\n" +
		"1️⃣ Сначала укажи свои API-ключи OKX.\n" +
		"2️⃣ Затем можешь запустить бота кнопкой «▶️ Запустить бота».\n\n" +
		"Отправь свои API-ключи в формате:\n" +
		"`OKX: apiKey; apiSecret; passphrase`"

	msg := tgbotapi.NewMessage(chatID, msgText)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = replyKb

	_, err = t.SendMessage(ctx, msg)
	return err
}
func (t *Telegram) handleTextMessage(ctx context.Context, msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	text := strings.TrimSpace(msg.Text)

	// 1) Ключи OKX
	if strings.HasPrefix(strings.ToUpper(text), "OKX:") {
		t.handleOkxKeys(ctx, msg)
		return
	}

	// 2) Гарантируем, что юзер есть
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, err = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	switch text {
	case "▶️ Запустить бота":
		go func() {
			runCtx := context.Background()                                // можно сделать per-user контекст, если захочешь
			if err := t.manager.RunForUser(runCtx, user, t); err != nil { // t реализует TelegramNotifier
				log.Printf("RunForUser error: %v", err)
				_, err = t.Send(runCtx, chatID, "❌ Не удалось запустить бота: "+err.Error())
				return
			}
			_, err = t.Send(runCtx, chatID, "✅ Бот запущен для этого аккаунта.")
		}()
		return

	case "⏹ Остановить бота":
		// Тут предполагаем, что у manager есть StopForUser.
		// Если пока нет — можно оставить заглушку и сделать TODO.
		if err := t.manager.StopForUser(ctx, user); err != nil {
			log.Printf("StopForUser error: %v", err)
			_, err = t.Send(ctx, chatID, "⚠️ Не удалось остановить бота: "+err.Error())
			return
		}
		_, err = t.Send(ctx, chatID, "🛑 Бот остановлен для этого аккаунта.")
		return

	case "⚙️ Настройки":
		t.handleSettingsMenu(ctx, chatID)
		return

	case "📊 Статус":
		// простая заглушка статуса, можно расширить
		_, err = t.Send(ctx, chatID, "ℹ️ Статус: бот работает в тестовом режиме.\nСкоро здесь будет подробный статус позиций/стратегии.")
		return
	}

	// дальше — прочий текст, если понадобится
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

	user, err := t.getUser(ctx, chatID)
	if err != nil {
		t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	user.TradingSettings.OKXAPIKey = parts[0]
	user.TradingSettings.OKXAPISecret = parts[1]
	user.TradingSettings.OKXPassphrase = parts[2]

	_ = t.repo.Update(ctx, user)

	t.bot.Send(tgbotapi.NewMessage(chatID, "✅ Ключи OKX сохранены. Теперь можно запускать торговлю."))
}

func (t *Telegram) handleSettingsMenu(ctx context.Context, chatID int64) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	text := fmt.Sprintf(
		"*Текущие настройки:*\n\n"+
			"Таймфрейм: `%s`\n"+
			"EMA: %d / %d\n"+
			"RSI: period=%d OB=%.0f OS=%.0f\n"+
			"Риск: %.2f%% на сделку\n"+
			"Размер позиции: %.2f%% от баланса\n"+
			"Плечо: x%d\n"+
			"Макс. позиций: %d\n",
		user.TradingSettings.Timeframe,
		user.TradingSettings.EMAShort, user.TradingSettings.EMALong,
		user.TradingSettings.RSIPeriod, user.TradingSettings.RSIOverbought, user.TradingSettings.RSIOSold,
		user.TradingSettings.RiskPct, user.TradingSettings.PositionPct,
		user.TradingSettings.Leverage,
		user.TradingSettings.MaxOpenPositions,
	)

	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⏱ Таймфрейм", "set_timeframe"),
			tgbotapi.NewInlineKeyboardButtonData("📉 Риск", "set_risk"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📏 Размер позиции", "set_position_pct"),
			tgbotapi.NewInlineKeyboardButtonData("⚙️ EMA/RSI", "set_ema_rsi"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔑 Ключи OKX", "set_okx"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = kb

	_, err = t.SendMessage(ctx, msg)
}

func (t *Telegram) handleCallback(ctx context.Context, chatID int64, cb *tgbotapi.CallbackQuery) {
	_, err := t.getUser(ctx, chatID)
	if err != nil {
		_, err = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	// отвечаем ТГ, чтобы убрать "часики" на кнопке
	_, _ = t.bot.Request(tgbotapi.NewCallback(cb.ID, ""))

	data := cb.Data

	// 1) Сначала обрабатываем кнопки настроек
	switch data {
	case "set_timeframe":
		t.handleSetTimeframe(ctx, chatID, cb.Message)
		return
	case "set_risk":
		t.handleSetRisk(ctx, chatID, cb.Message)
		return
	case "set_position_pct":
		t.handleSetPositionPct(ctx, chatID, cb.Message)
		return
	case "set_ema_rsi":
		t.handleSetEmaRsi(ctx, chatID, cb.Message)
		return
	case "set_okx":
		t.handleSetOkx(ctx, chatID, cb.Message)
		return
	}
	// 2) EMA/RSI редактирование
	if strings.HasPrefix(data, "ema_rsi:") {
		t.handleEmaRsiAdjust(ctx, chatID, cb.Message, data)
		return
	}
	// 2) Подтверждения входа/пропуска: CONF::token / REJ::token
	if strings.Contains(data, "::") {
		t.handleConfirmCallback(chatID, data)
		return
	}
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

func (t *Telegram) handleSetRisk(ctx context.Context, chatID int64, msg *tgbotapi.Message) {
	_, _ = t.Send(ctx, chatID, "Введи риск в процентах, например: `1.0` (это 1% на сделку).")
}

func (t *Telegram) handleSetPositionPct(ctx context.Context, chatID int64, msg *tgbotapi.Message) {
	_, _ = t.Send(ctx, chatID, "Введи размер позиции в процентах от баланса, например: `1.0`.")
}

func (t *Telegram) handleSetEmaRsi(ctx context.Context, chatID int64, msg *tgbotapi.Message) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, err = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID,
		msg.MessageID,
		formatEmaRsiText(user),
		buildEmaRsiKeyboard(),
	)
	edit.ParseMode = "Markdown"

	if _, err := t.bot.Send(edit); err != nil {
		log.Printf("handleSetEmaRsi edit error: %v", err)
	}
}

func (t *Telegram) handleSetOkx(ctx context.Context, chatID int64, msg *tgbotapi.Message) {
	_, _ = t.Send(ctx, chatID, "Отправь ключи OKX в формате:\n`OKX: apiKey; apiSecret; passphrase`")
}

// handleConfirmCallback обрабатывает callback-и вида CONF::token / REJ::token.
func (t *Telegram) handleConfirmCallback(chatID int64, data string) {
	verb, token := parseConfirmData(data)
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

	_ = t.editReplyMarkupRemove(chatID, p.msgID)
	_ = t.editText(chatID, p.msgID, fmt.Sprintf("%s\n\n%s %s", p.prompt, emoji, status))

	t.mu.Lock()
	delete(t.pendings, token)
	t.mu.Unlock()
}
func parseConfirmData(data string) (verb, token string) {
	for i := 0; i < len(data); i++ {
		if i+1 < len(data) && data[i] == ':' && data[i+1] == ':' {
			return data[:i], data[i+2:]
		}
	}
	return "", ""
}
func formatEmaRsiText(user *models.UserSettings) string {
	ts := user.TradingSettings
	return fmt.Sprintf(
		"*Редактор EMA/RSI*\n\n"+
			"Таймфрейм: `%s`\n\n"+
			"*EMA*\n"+
			"  Короткая: `%d`\n"+
			"  Длинная:  `%d`\n\n"+
			"*RSI*\n"+
			"  Период:   `%d`\n"+
			"  OB (перекупленность): `%0.f`\n"+
			"  OS (перепроданность): `%0.f`",
		ts.Timeframe,
		ts.EMAShort,
		ts.EMALong,
		ts.RSIPeriod,
		ts.RSIOverbought,
		ts.RSIOSold,
	)
}

func buildEmaRsiKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("EMA S −", "ema_rsi:ema_s:-1"),
			tgbotapi.NewInlineKeyboardButtonData("EMA S +", "ema_rsi:ema_s:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("EMA L −", "ema_rsi:ema_l:-1"),
			tgbotapi.NewInlineKeyboardButtonData("EMA L +", "ema_rsi:ema_l:+1"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("RSI OB −5", "ema_rsi:rsi_ob:-5"),
			tgbotapi.NewInlineKeyboardButtonData("RSI OB +5", "ema_rsi:rsi_ob:+5"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("RSI OS −5", "ema_rsi:rsi_os:-5"),
			tgbotapi.NewInlineKeyboardButtonData("RSI OS +5", "ema_rsi:rsi_os:+5"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Готово", "ema_rsi:done"),
			tgbotapi.NewInlineKeyboardButtonData("↩️ Назад", "ema_rsi:back"),
		),
	)
}
func (t *Telegram) handleEmaRsiAdjust(
	ctx context.Context,
	chatID int64,
	msg *tgbotapi.Message,
	data string,
) {
	user, err := t.getUser(ctx, chatID)
	if err != nil {
		_, _ = t.Send(ctx, chatID, "Настройки не найдены, попробуй /start")
		return
	}

	// data вида: "ema_rsi:ema_s:-1" / "ema_rsi:rsi_ob:+5" / "ema_rsi:done"
	parts := strings.Split(data, ":")
	if len(parts) < 2 {
		return
	}

	action := parts[1]

	// "Готово" и "Назад"
	if action == "done" {
		// просто перерисуем основное меню настроек
		t.handleSettingsMenu(ctx, chatID)
		return
	}
	if action == "back" {
		t.handleSettingsMenu(ctx, chatID)
		return
	}

	// Остальные: ema_s, ema_l, rsi_ob, rsi_os
	if len(parts) != 3 {
		return
	}
	deltaStr := parts[2]
	delta, err := strconv.Atoi(deltaStr)
	if err != nil {
		return
	}

	ts := &user.TradingSettings

	switch action {
	case "ema_s":
		ts.EMAShort += delta
		if ts.EMAShort < 1 {
			ts.EMAShort = 1
		}
		// гарантируем EMAShort < EMALong
		if ts.EMAShort >= ts.EMALong {
			ts.EMAShort = ts.EMALong - 1
			if ts.EMAShort < 1 {
				ts.EMAShort = 1
			}
		}
	case "ema_l":
		ts.EMALong += delta
		if ts.EMALong <= ts.EMAShort {
			ts.EMALong = ts.EMAShort + 1
		}
	case "rsi_ob":
		ts.RSIOverbought += float64(delta)
		if ts.RSIOverbought < 50 {
			ts.RSIOverbought = 50
		}
		if ts.RSIOverbought > 90 {
			ts.RSIOverbought = 90
		}
	case "rsi_os":
		ts.RSIOSold += float64(delta)
		if ts.RSIOSold < 10 {
			ts.RSIOSold = 10
		}
		if ts.RSIOSold > 50 {
			ts.RSIOSold = 50
		}
	default:
		return
	}

	if err := t.repo.Update(ctx, user); err != nil {
		log.Printf("update user ema/rsi error: %v", err)
	}

	// Перерисовываем то же сообщение
	edit := tgbotapi.NewEditMessageTextAndMarkup(
		chatID,
		msg.MessageID,
		formatEmaRsiText(user),
		buildEmaRsiKeyboard(),
	)
	edit.ParseMode = "Markdown"

	if _, err := t.bot.Send(edit); err != nil {
		log.Printf("handleEmaRsiAdjust edit error: %v", err)
	}
}
