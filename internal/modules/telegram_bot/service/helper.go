package service

import (
	"fmt"
	"strconv"
	"strings"

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

func f2(v float64) string { // для красивого вывода
	return fmt.Sprintf("%.2f", v)
}

func mustInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

func mustFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64)
	return v
}
