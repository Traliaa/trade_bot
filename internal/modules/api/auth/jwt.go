package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type UserIDContextKey struct{}
type Claims struct {
	Sub        string `json:"sub"`
	TgUserID   int64  `json:"tg_user_id"`
	TgUsername string `json:"tg_username,omitempty"`
	Exp        int64  `json:"exp"`
	Iat        int64  `json:"iat"`
}

func SignHS256(secret []byte, c Claims) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}

	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(c)

	enc := base64.RawURLEncoding
	h := enc.EncodeToString(hb)
	p := enc.EncodeToString(cb)

	msg := h + "." + p

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	sig := enc.EncodeToString(mac.Sum(nil))

	return msg + "." + sig, nil
}

func VerifyHS256(secret []byte, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("bad token format")
	}

	msg := parts[0] + "." + parts[1]

	enc := base64.RawURLEncoding
	sig, err := enc.DecodeString(parts[2])
	if err != nil {
		return Claims{}, errors.New("bad signature encoding")
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(msg))
	want := mac.Sum(nil)
	if !hmac.Equal(sig, want) {
		return Claims{}, errors.New("bad signature")
	}

	payload, err := enc.DecodeString(parts[1])
	if err != nil {
		return Claims{}, errors.New("bad payload encoding")
	}

	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return Claims{}, errors.New("bad payload json")
	}

	if c.Exp > 0 && time.Now().Unix() > c.Exp {
		return Claims{}, errors.New("token expired")
	}

	return c, nil
}
