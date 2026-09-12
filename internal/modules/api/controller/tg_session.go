package controller

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
	"trade_bot/internal/modules/api/auth"
	"trade_bot/internal/modules/api/validate"
	"trade_bot/internal/modules/config"
)

type TgSessionController struct {
	cfg       *config.Config
	JWTSecret []byte
}

type tgSessionReq struct {
	InitData string `json:"initData"`
}

type tgSessionResp struct {
	Token string `json:"token"`
}

func NewTgSessionController(cfg *config.Config, jwtSecret []byte) *TgSessionController {
	return &TgSessionController{
		cfg:       cfg,
		JWTSecret: jwtSecret,
	}
}

func (c *TgSessionController) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req tgSessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InitData == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ok, v := validate.ValidateInitData(req.InitData, c.cfg.Telegram.Token)
	if !ok {
		log.Println("unauthorized: ValidateInitData")

		http.Error(w, "unauthorized: ValidateInitData", http.StatusUnauthorized)
		return
	}

	userJSON := v.Get("user")
	if userJSON == "" {
		log.Println("unauthorized: no user")

		http.Error(w, "unauthorized: no user", http.StatusUnauthorized)
		return
	}

	var u struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal([]byte(userJSON), &u); err != nil || u.ID == 0 {
		log.Println("unauthorized: bad user")

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

func (c *TgSessionController) CreateDevSession(w http.ResponseWriter, r *http.Request) {
	appEnv := os.Getenv("APP_ENV")
	if (appEnv != "dev" && appEnv != "local") || os.Getenv("ENABLE_DEV_AUTH") != "true" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	userID, err := strconv.ParseInt(os.Getenv("DEV_TELEGRAM_USER_ID"), 10, 64)
	if err != nil || userID <= 0 {
		http.Error(w, "dev user is not configured", http.StatusServiceUnavailable)
		return
	}
	username := "dev"

	// Можно переопределить через env
	if s := os.Getenv("DEV_TELEGRAM_USERNAME"); s != "" {
		username = s
	}

	now := time.Now().Unix()
	claims := auth.Claims{
		Sub:        strconv.FormatInt(userID, 10),
		TgUserID:   userID,
		TgUsername: username,
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
