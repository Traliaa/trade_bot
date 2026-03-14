package validate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ValidateInitData validates Telegram WebApp initData using bot token.
//
// Algorithm:
// 1) Parse query params
// 2) Extract "hash", remove it from params
// 3) Build data_check_string = "key=value\n..." sorted by key
// 4) secret_key = HMAC_SHA256("WebAppData", bot_token)
// 5) check_hash = HMAC_SHA256(secret_key, data_check_string) hex
func ValidateInitData(initData string, botToken string) (bool, url.Values) {
	v, err := url.ParseQuery(initData)
	if err != nil {
		return false, nil
	}

	recvHash := v.Get("hash")
	if recvHash == "" {
		return false, v
	}

	v.Del("hash")

	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v.Get(k))
	}
	dataCheckString := b.String()

	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(botToken))
	secretKey := secret.Sum(nil)

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(dataCheckString))
	sum := mac.Sum(nil)
	want := hex.EncodeToString(sum)

	if !hmac.Equal([]byte(strings.ToLower(recvHash)), []byte(strings.ToLower(want))) {
		return false, v
	}

	authDateStr := v.Get("auth_date")
	if authDateStr == "" {
		return false, v
	}

	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil || authDate <= 0 {
		return false, v
	}

	now := time.Now().Unix()

	// допустимое окно 1 час
	if now-authDate > 3600 {
		return false, v
	}

	// защита от слишком "будущего" времени
	if authDate-now > 300 {
		return false, v
	}

	return true, v
}
