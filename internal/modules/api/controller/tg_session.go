package controller

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
	"trade_bot/internal/modules/api/auth"
	"trade_bot/internal/modules/api/validate"
)

type TgSessionController struct {
	BotToken  string
	JWTSecret []byte
}

type tgSessionReq struct {
	InitData string `json:"initData"`
}

type tgSessionResp struct {
	Token string `json:"token"`
}

func NewTgSessionController(botToken string, jwtSecret []byte) *TgSessionController {
	return &TgSessionController{
		BotToken:  botToken,
		JWTSecret: jwtSecret,
	}
}

func (c *TgSessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req tgSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InitData == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ok, v := validate.ValidateInitData(req.InitData, c.BotToken)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	userJSON := v.Get("user")
	if userJSON == "" {
		http.Error(w, "unauthorized: no user", http.StatusUnauthorized)
		return
	}

	var u struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(userJSON), &u); err != nil || u.ID == 0 {
		http.Error(w, "unauthorized: bad user", http.StatusUnauthorized)
		return
	}

	now := time.Now().Unix()
	claims := auth.Claims{
		Sub:        strconv.FormatInt(u.ID, 10),
		TgUserID:   u.ID,
		TgUsername: u.Username,
		Iat:        now,
		Exp:        now + 60*60*24*7, // 7 дней
	}

	tok, err := auth.SignHS256(c.JWTSecret, claims)
	if err != nil {
		http.Error(w, "failed to sign token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tgSessionResp{Token: tok})
}
