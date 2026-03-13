package validate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
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

	// SHA256(bot_token)
	hash := sha256.Sum256([]byte(botToken))
	secretKey := hash[:]

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(dataCheckString))

	sum := mac.Sum(nil)
	want := hex.EncodeToString(sum)

	return hmac.Equal([]byte(recvHash), []byte(want)), v
}
