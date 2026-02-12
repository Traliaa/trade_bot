package service

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func onOff(v bool) string {
	if v {
		return "вкл ✅"
	}
	return "выкл ⭕️"
}
func btn(text, data string) tgbotapi.InlineKeyboardButton {
	return tgbotapi.NewInlineKeyboardButtonData(text, data)
}
