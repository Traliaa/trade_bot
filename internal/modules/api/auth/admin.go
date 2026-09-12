package auth

import (
	"os"
	"strconv"
)

// DefaultAdminTelegramUserID preserves the existing bot administrator.
const DefaultAdminTelegramUserID int64 = 213532199

func IsAdmin(userID int64) bool {
	adminID := DefaultAdminTelegramUserID
	if value, configured := os.LookupEnv("ADMIN_TELEGRAM_USER_ID"); configured {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return false
		}
		adminID = parsed
	}
	return userID > 0 && userID == adminID
}
