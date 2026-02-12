package service

import (
	"context"

	tgbot "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// ParseMode для публичных сообщений (у тебя тексты в стиле *жирный*)
const serviceParseMode = tgbot.ModeMarkdown

// SendServiceText — отправляет "продуктовый" текст в сервисный/публичный чат и возвращает messageID.
func (t *Telegram) SendServiceText(ctx context.Context, text string) (int, error) {
	if t.cfg.ServiceTelegramChatID == 0 {
		return 0, nil
	}

	msg := tgbot.NewMessage(int64(t.cfg.ServiceTelegramChatID), text)
	msg.ParseMode = serviceParseMode
	msg.DisableWebPagePreview = true

	sent, err := t.SendMessage(ctx, msg) // у тебя уже есть метод-обёртка
	if err != nil {
		return 0, err
	}
	return sent.MessageID, nil
}

// EditServiceText — редактирует ранее отправленное "продуктовое" сообщение по messageID.
func (t *Telegram) EditServiceText(ctx context.Context, messageID int, text string) error {
	if t.cfg.ServiceTelegramChatID == 0 || messageID == 0 {
		return nil
	}

	edit := tgbot.NewEditMessageText(int64(t.cfg.ServiceTelegramChatID), messageID, text)
	edit.ParseMode = serviceParseMode
	edit.DisableWebPagePreview = true

	_, err := t.bot.Request(edit)
	return err
}
